package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
)

func (s *Store) CommerceCatalog(ctx context.Context) (commerce.Catalog, error) {
	if s.Edition == config.Personal {
		return commerce.DefaultCatalog(), nil
	}
	raw, err := s.Setting(ctx, "commerce_suite_catalog")
	if err != nil {
		return commerce.Catalog{}, err
	}
	if len(raw) == 0 || string(raw) == "{}" {
		return commerce.DefaultCatalog(), nil
	}
	var c commerce.Catalog
	if json.Unmarshal(raw, &c) != nil || c.Validate() != nil {
		return c, ErrConflict
	}
	return c, nil
}

// The mutation runs under the shared admin lock, preventing lost partial edits.
func (s *Store) UpdateCommerceCatalog(ctx context.Context, actor, requestID string, mutate func(*commerce.Catalog) error) (commerce.Catalog, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return commerce.Catalog{}, err
	}
	defer tx.Rollback(context.Background())
	c := commerce.DefaultCatalog()
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT value FROM app_settings WHERE key='commerce_suite_catalog' FOR UPDATE").Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	if err == nil && json.Unmarshal(raw, &c) != nil {
		return c, ErrConflict
	}
	if err = mutate(&c); err != nil {
		return c, err
	}
	if err = c.Validate(); err != nil {
		return c, err
	}
	c.UpdatedAt = time.Now().UTC()
	if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES('commerce_suite_catalog',$1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_by=$2,version=app_settings.version+1,updated_at=now()`, creative.JSON(c), actor); err != nil {
		return c, err
	}
	if err = adminAudit(ctx, tx, actor, "commerce_catalog_update", "commerce_catalog", "commerce_suite_catalog", requestID, map[string]any{"platforms": len(c.Platforms), "templates": len(c.Templates)}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
