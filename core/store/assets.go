package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type Asset struct {
	ID               string          `json:"id"`
	UserID           string          `json:"-"`
	ProjectID        string          `json:"project_id,omitempty"`
	TaskID           string          `json:"task_id,omitempty"`
	ImageType        string          `json:"image_type"`
	StorageKey       string          `json:"-"`
	OriginalFilename string          `json:"original_filename"`
	ContentType      string          `json:"content_type"`
	FileSize         int64           `json:"file_size"`
	Width            int             `json:"width"`
	Height           int             `json:"height"`
	SHA256           string          `json:"sha256"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"created_at"`
}

const assetColumns = `id,user_id,COALESCE(project_id,''),COALESCE(task_id,''),image_type,storage_key,original_filename,content_type,file_size,width,height,sha256,metadata,created_at`

func scanAsset(row pgx.Row) (Asset, error) {
	var a Asset
	err := row.Scan(&a.ID, &a.UserID, &a.ProjectID, &a.TaskID, &a.ImageType, &a.StorageKey, &a.OriginalFilename, &a.ContentType, &a.FileSize, &a.Width, &a.Height, &a.SHA256, &a.Metadata, &a.CreatedAt)
	return a, normalizeError(err)
}
func (s *Store) Asset(ctx context.Context, id string) (Asset, error) {
	return scanAsset(s.Pool.QueryRow(ctx, "SELECT "+assetColumns+" FROM assets WHERE id=$1 AND deleted_at IS NULL AND image_type<>'worker-candidate'", id))
}
func (s *Store) OwnedAsset(ctx context.Context, user, id string) (Asset, error) {
	return scanAsset(s.Pool.QueryRow(ctx, "SELECT "+assetColumns+" FROM assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND image_type<>'worker-candidate'", id, user))
}

func (s *Store) AssetStorageKeyExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE storage_key=$1)`, key).Scan(&exists)
	return exists, err
}
func (s *Store) SaveAsset(ctx context.Context, a Asset) (Asset, error) {
	if len(a.Metadata) == 0 {
		a.Metadata = json.RawMessage(`{}`)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(context.Background())
	if a.ProjectID != "" {
		var owned bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)", a.ProjectID, a.UserID).Scan(&owned); err != nil {
			return Asset{}, err
		}
		if !owned {
			return Asset{}, ErrNotFound
		}
	}
	if a.TaskID != "" {
		var owned bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND user_id=$2)", a.TaskID, a.UserID).Scan(&owned); err != nil {
			return Asset{}, err
		}
		if !owned {
			return Asset{}, ErrNotFound
		}
	}
	a, err = scanAsset(tx.QueryRow(ctx, `INSERT INTO assets(id,user_id,project_id,task_id,image_type,storage_key,original_filename,content_type,file_size,width,height,sha256,metadata) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+assetColumns, a.ID, a.UserID, a.ProjectID, a.TaskID, a.ImageType, a.StorageKey, a.OriginalFilename, a.ContentType, a.FileSize, a.Width, a.Height, a.SHA256, []byte(a.Metadata)))
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}
