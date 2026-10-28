package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) saveImage(ctx context.Context, userID, projectID, taskID, kind, filename string, reader io.Reader) (store.Asset, error) {
	normalize := s.NormalizeImage
	if normalize == nil {
		normalize = func(_ context.Context, input io.Reader) (storage.Image, error) { return storage.NormalizeImage(input) }
	}
	img, err := normalize(ctx, reader)
	if err != nil {
		return store.Asset{}, err
	}
	id := security.NewID("asset")
	key, err := s.Files.WriteImage(id, img)
	if err != nil {
		return store.Asset{}, err
	}
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)
	if len(filename) > 200 {
		filename = "image." + img.Extension
	}
	meta, _ := json.Marshal(map[string]any{"source_sha256": img.SourceSHA256, "format": img.Extension})
	a, err := s.Store.SaveAsset(ctx, store.Asset{ID: id, UserID: userID, ProjectID: projectID, TaskID: taskID, ImageType: kind, StorageKey: key, OriginalFilename: filename, ContentType: img.ContentType, FileSize: int64(len(img.Bytes)), Width: img.Width, Height: img.Height, SHA256: img.SHA256, Metadata: meta})
	if err != nil {
		s.removeUncommittedAsset(key)
		return store.Asset{}, err
	}
	return a, nil
}

// A commit error can mean the database committed but its reply was lost. Never
// remove a file until a fresh connection confirms it has no durable asset row.
func (s *Server) removeUncommittedAsset(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := s.Store.AssetStorageKeyExists(ctx, key)
	if err == nil && !exists {
		_ = s.Files.Remove(key)
	}
}

func (s *Server) assetView(a store.Asset) map[string]any {
	return map[string]any{"id": a.ID, "image_type": a.ImageType, "original_filename": a.OriginalFilename, "url": storage.AssetURL(s.Config.PublicBaseURL, s.Config.WorkerSigningKey, a.ID, a.UserID, time.Now()), "content_type": a.ContentType, "image_format": strings.TrimPrefix(a.ContentType, "image/"), "file_size": a.FileSize, "width": a.Width, "height": a.Height, "sha256": a.SHA256, "created_at": a.CreatedAt}
}

func readSingleImage(w http.ResponseWriter, r *http.Request) ([]byte, string, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, storage.MaxImageBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, r, 400, "INVALID_MULTIPART", "Expected multipart form data")
		return nil, "", "", false
	}
	var data []byte
	filename := ""
	kind := "reference"
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fail(w, r, 400, "INVALID_MULTIPART", "Could not read upload")
			return nil, "", "", false
		}
		switch part.FormName() {
		case "file":
			if data != nil {
				part.Close()
				fail(w, r, 400, "INVALID_MULTIPART", "Exactly one image is required")
				return nil, "", "", false
			}
			filename = part.FileName()
			data, err = io.ReadAll(io.LimitReader(part, storage.MaxImageBytes+1))
			part.Close()
			if err != nil || int64(len(data)) > storage.MaxImageBytes {
				fail(w, r, 413, "IMAGE_TOO_LARGE", "Image exceeds the 25 MiB limit")
				return nil, "", "", false
			}
		case "imageType":
			v, err := io.ReadAll(io.LimitReader(part, 81))
			part.Close()
			if err != nil || len(v) > 80 {
				fail(w, r, 400, "INVALID_REQUEST", "Invalid image type")
				return nil, "", "", false
			}
			kind = string(v)
		default:
			part.Close()
			fail(w, r, 400, "INVALID_MULTIPART", "Unexpected upload field")
			return nil, "", "", false
		}
	}
	if len(data) == 0 || filename == "" {
		fail(w, r, 400, "IMAGE_REQUIRED", "Choose an image")
		return nil, "", "", false
	}
	return data, filename, kind, true
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) { s.uploadImage(w, r, "", false) }
func (s *Server) uploadProjectImage(w http.ResponseWriter, r *http.Request) {
	s.uploadImage(w, r, r.PathValue("id"), false)
}
func (s *Server) uploadAvatar(w http.ResponseWriter, r *http.Request) { s.uploadImage(w, r, "", true) }

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request, projectID string, avatar bool) {
	data, filename, kind, valid := readSingleImage(w, r)
	if !valid {
		return
	}
	if avatar {
		kind = "avatar"
	}
	u := currentUser(r)
	a, err := s.saveImage(r.Context(), u.ID, projectID, "", kind, filename, bytes.NewReader(data))
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInvalidImage):
			fail(w, r, 415, "IMAGE_FORMAT_UNSUPPORTED", "Use a valid JPEG, PNG or WebP image")
		case errors.Is(err, storage.ErrImageTooLarge):
			fail(w, r, 413, "IMAGE_TOO_LARGE", "Image exceeds the 25 MiB or 50 megapixel limit")
		default:
			s.databaseError(w, r, err)
		}
		return
	}
	if avatar {
		if _, err = s.Store.Pool.Exec(r.Context(), "UPDATE users SET avatar_asset_id=$2,updated_at=now() WHERE id=$1", u.ID, a.ID); err != nil {
			s.databaseError(w, r, err)
			return
		}
		result := profile(u)
		result["avatar_url"] = s.assetView(a)["url"]
		ok(w, r, result)
		return
	}
	ok(w, r, s.assetView(a))
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.Asset(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, 404, "NOT_FOUND", "File not found")
		return
	}
	expires, _ := strconv.ParseInt(r.URL.Query().Get("expiresAt"), 10, 64)
	if !storage.VerifyAsset(s.Config.WorkerSigningKey, a.ID, a.UserID, r.URL.Query().Get("signature"), expires, time.Now()) {
		fail(w, r, 404, "NOT_FOUND", "File link is invalid or expired")
		return
	}
	f, err := s.Files.Open(a.StorageKey)
	if err != nil {
		fail(w, r, 404, "NOT_FOUND", "File not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.ContentType)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.OriginalFilename}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("ETag", `"`+a.SHA256+`"`)
	http.ServeContent(w, r, a.OriginalFilename, a.CreatedAt, f)
}
