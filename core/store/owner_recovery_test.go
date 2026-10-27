package store

import (
	"context"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
)

func TestPostgresLocalOwnerRecoveryRevokesSessionsAndRefusesMembers(t *testing.T) {
	for _, edition := range []config.Edition{config.Personal, config.Platform} {
		t.Run(string(edition), func(t *testing.T) {
			s := testStore(t, edition)
			ctx := context.Background()
			u, err := s.Initialize(ctx, "recover@example.invalid", "fixture-before", "Owner")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RecoverOwnerPassword(ctx, u.Email, "fixture-after"); err != nil {
				t.Fatal(err)
			}
			updated, err := s.UserByID(ctx, u.ID)
			if err != nil || updated.PasswordHash != "fixture-after" || updated.AuthVersion != u.AuthVersion+1 {
				t.Fatal("recovery did not invalidate sessions")
			}
			if edition == config.Platform {
				member, err := s.CreateUser(ctx, "member-recover@example.invalid", "fixture-member", "Member")
				if err != nil {
					t.Fatal(err)
				}
				if err = s.RecoverOwnerPassword(ctx, member.Email, "not-allowed"); err == nil {
					t.Fatal("non-owner recovered through owner CLI")
				}
			}
			var count int
			if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='owner.password.recovery'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("missing recovery audit")
			}
		})
	}
}
