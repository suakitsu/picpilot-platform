package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/jobs"
	"github.com/suakitsu/picpilot-platform/core/provider"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
)

const assetURIPrefix = "picpilot-asset:"

// prepareTextInput replaces image data/URLs with owned asset references. Prompt
// text is not rewritten; only typed multimodal image fields are inspected.
func (s *Server) prepareTextInput(ctx context.Context, userID string, input jobs.TextInput) (jobs.TextInput, error) {
	return transformTextImages(input, func(uri string) (string, error) {
		if id, ok := strings.CutPrefix(uri, assetURIPrefix); ok {
			if _, err := s.Store.OwnedAsset(ctx, userID, id); err != nil {
				return "", err
			}
			return uri, nil
		}
		if id := s.ownedAssetURLID(uri); id != "" {
			if _, err := s.Store.OwnedAsset(ctx, userID, id); err != nil {
				return "", err
			}
			return assetURIPrefix + id, nil
		}
		var data []byte
		if strings.HasPrefix(uri, "data:image/") {
			header, encoded, found := strings.Cut(uri, ",")
			if !found || !strings.HasSuffix(header, ";base64") || len(encoded) > int(storage.MaxImageBytes*4/3+8) {
				return "", store.ErrConflict
			}
			var err error
			data, err = base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return "", store.ErrConflict
			}
		} else {
			// Provider endpoints may be private/local by explicit configuration, but
			// arbitrary user-supplied reference URLs stay behind the strict SSRF
			// policy even when the instance has a local Provider configured.
			gateway, err := provider.NewGateway(provider.Options{MaxImageBytes: storage.MaxImageBytes, MaxPixels: storage.MaxImagePixels, Timeout: 30 * time.Second})
			if err != nil {
				return "", err
			}
			defer gateway.CloseIdleConnections()
			img, err := gateway.DownloadImage(ctx, uri)
			if err != nil {
				return "", err
			}
			data = img.Bytes
		}
		a, err := s.saveImage(ctx, userID, "", "", "reference", "reference.png", bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		return assetURIPrefix + a.ID, nil
	})
}

func (s *Server) ownedAssetURLID(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	base, err := url.Parse(s.Config.PublicBaseURL)
	if err != nil {
		return ""
	}
	if u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil {
		return ""
	}
	id, ok := strings.CutPrefix(u.Path, "/api/files/assets/")
	if !ok || strings.ContainsAny(id, "/\\") {
		return ""
	}
	return id
}

// Worker requests receive short HTTPS URLs; direct HTTP adapters receive inline
// bytes only in memory. Stored task payloads never contain the encoded image.
func (s *Server) materializeTextInput(ctx context.Context, userID string, input jobs.TextInput, worker bool) (jobs.TextInput, error) {
	var total int64
	return transformTextImages(input, func(uri string) (string, error) {
		id, ok := strings.CutPrefix(uri, assetURIPrefix)
		if !ok {
			return "", errors.New("text image must be an owned asset")
		}
		a, err := s.Store.OwnedAsset(ctx, userID, id)
		if err != nil {
			return "", err
		}
		total += a.FileSize
		if total > 20<<20 {
			return "", storage.ErrImageTooLarge
		}
		if worker {
			if !strings.HasPrefix(s.Config.PublicBaseURL, "https://") {
				return "", errors.New("worker images require an HTTPS public URL")
			}
			return storage.AssetURL(s.Config.PublicBaseURL, s.Config.WorkerSigningKey, a.ID, a.UserID, time.Now()), nil
		}
		data, err := s.Files.Read(a.StorageKey, storage.MaxImageBytes)
		if err != nil {
			return "", err
		}
		return "data:" + a.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	})
}

func transformTextImages(input jobs.TextInput, transform func(string) (string, error)) (jobs.TextInput, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return jobs.TextInput{}, store.ErrConflict
	}
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&doc) != nil {
		return jobs.TextInput{}, store.ErrConflict
	}
	count := 0
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				if err := visit(item); err != nil {
					return err
				}
			}
		case map[string]any:
			typ, _ := v["type"].(string)
			if typ == "image_url" || typ == "input_image" {
				count++
				if count > 4 {
					return storage.ErrImageTooLarge
				}
				if uri, ok := v["image_url"].(string); ok {
					next, err := transform(uri)
					if err != nil {
						return err
					}
					v["image_url"] = next
				} else if obj, ok := v["image_url"].(map[string]any); ok {
					uri, ok := obj["url"].(string)
					if !ok {
						return store.ErrConflict
					}
					next, err := transform(uri)
					if err != nil {
						return err
					}
					obj["url"] = next
				} else {
					return store.ErrConflict
				}
				return nil
			}
			for key, item := range v {
				if key == "messages" || key == "input" || key == "content" {
					if err := visit(item); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visit(doc); err != nil {
		return jobs.TextInput{}, err
	}
	raw, err = json.Marshal(doc)
	if err != nil {
		return jobs.TextInput{}, err
	}
	var result jobs.TextInput
	if json.Unmarshal(raw, &result) != nil {
		return jobs.TextInput{}, store.ErrConflict
	}
	return result, nil
}
