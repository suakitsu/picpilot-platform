package security

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestVaultBindsCredentialsToTheirOwner(t *testing.T) {
	v, err := NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := v.Encrypt("user-a/provider-1", "test-only-provider-credential")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "test-only") {
		t.Fatal("plaintext leaked")
	}
	if decoded, err := v.Decrypt("user-a/provider-1", encoded); err != nil || decoded != "test-only-provider-credential" {
		t.Fatal("roundtrip failed")
	}
	if _, err := v.Decrypt("user-b/provider-1", encoded); err == nil {
		t.Fatal("cross-owner substitution accepted")
	}
	if _, err := v.Decrypt("user-a/provider-1", encoded[:len(encoded)-6]+"broken"); err == nil {
		t.Fatal("tampering accepted")
	}
}

func TestSessionsRejectExpiredAndWrongIssuer(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	s, _ := NewSessions(key, "picpilot-test-platform")
	now := time.Now().UTC().Truncate(time.Second)
	token, err := s.Issue("test-user", 3, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Parse(token, now)
	if err != nil || claims.Subject != "test-user" || claims.Version != 3 {
		t.Fatal("session parse failed")
	}
	if _, err := s.Parse(token, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired token accepted")
	}
	other, _ := NewSessions(key, "picpilot-test-personal")
	if _, err := other.Parse(token, now); err == nil {
		t.Fatal("cross-edition token accepted")
	}
}

func TestPasswordAndTokenUtilities(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	hash, err := HashPassword("test-only-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "test-only-long-password") || VerifyPassword(hash, "incorrect") {
		t.Fatal("password verification failed")
	}
	a, _ := RandomToken("test_", 32)
	b, _ := RandomToken("test_", 32)
	if a == b || !ConstantEqual(a, a) || ConstantEqual(a, b) {
		t.Fatal("token comparison failed")
	}
	if TokenHash([]byte("pepper1"), a) == TokenHash([]byte("pepper2"), a) {
		t.Fatal("pepper ignored")
	}
}
