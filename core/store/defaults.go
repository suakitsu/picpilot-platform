package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func seedDisabledLocalProviders(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `INSERT INTO providers(owner_scope,id,name,capability,protocol,model,enabled,priority,execution_mode,max_concurrency,timeout_seconds,local_wait_seconds,offline_action,lease_seconds)
 VALUES ('platform','local-cliproxy-image','Local Image Worker','image','openai_images_generations','gpt-image-2',false,-100,'pull_worker',2,600,30,'fallback',120),
 ('platform','local-cliproxy-text','Local Text Worker','text','openai_chat_completions','gpt-5.5',false,-100,'pull_worker',4,90,30,'fallback',120)
 ON CONFLICT(owner_scope,id) DO NOTHING`)
	return err
}
