package server

import (
	"context"
	"encoding/json"
	"io"

	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) saveMedia(ctx context.Context, userID, projectID, taskID, kind, filename, expectedMIME string, reader io.Reader) (store.Asset, error) {
	id := security.NewID("asset")
	media, err := s.Files.WriteMedia(id, reader, expectedMIME)
	if err != nil {
		return store.Asset{}, err
	}
	meta, _ := json.Marshal(map[string]any{"format": media.Extension})
	a, err := s.Store.SaveAsset(ctx, store.Asset{ID: id, UserID: userID, ProjectID: projectID, TaskID: taskID, ImageType: kind, StorageKey: media.StorageKey, OriginalFilename: storage.SafeFilename(filename, "media."+media.Extension), ContentType: media.ContentType, FileSize: media.FileSize, SHA256: media.SHA256, Metadata: meta})
	if err != nil {
		s.removeUncommittedAsset(media.StorageKey)
		return store.Asset{}, err
	}
	return a, nil
}
