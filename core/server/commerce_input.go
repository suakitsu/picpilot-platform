package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/suakitsu/picpilot-platform/core/commerce"
	"github.com/suakitsu/picpilot-platform/core/creative"
	"github.com/suakitsu/picpilot-platform/core/storage"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func readCommerceInput(w http.ResponseWriter, r *http.Request, dst any, translation bool) ([]creativeUpload, json.RawMessage, error) {
	var raw []byte
	uploads := []creativeUpload{}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, nil, invalidWorkflow()
		}
		fields := map[string]any{}
		total := 0
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, nil, invalidWorkflow()
			}
			field := part.FormName()
			if part.FileName() != "" {
				if translation && field != "images" || !translation && field != "reference" && field != "references" {
					part.Close()
					return nil, nil, invalidWorkflow()
				}
				data, err := io.ReadAll(io.LimitReader(part, storage.MaxImageBytes+1))
				part.Close()
				total += len(data)
				if err != nil || len(data) == 0 || int64(len(data)) > storage.MaxImageBytes || total > 28<<20 || len(uploads) >= 10 {
					return nil, nil, invalidWorkflow()
				}
				uploads = append(uploads, creativeUpload{data: data, name: part.FileName()})
			} else {
				data, err := io.ReadAll(io.LimitReader(part, 128<<10+1))
				part.Close()
				if err != nil || len(data) > 128<<10 {
					return nil, nil, invalidWorkflow()
				}
				if _, exists := fields[field]; exists {
					return nil, nil, invalidWorkflow()
				}
				switch field {
				case "product_info", "productInfo", "selected_slots", "selectedSlots", "overlay_options", "overlayOptions", "reference_image_ids", "referenceImageIds", "reference_asset_ids", "referenceAssetIds":
					var value any
					if json.Unmarshal(data, &value) != nil {
						return nil, nil, invalidWorkflow()
					}
					fields[field] = value
				case "platform", "resolution", "reference_url", "referenceUrl", "target_language", "targetLanguage", "style":
					fields[field] = string(data)
				default:
					return nil, nil, invalidWorkflow()
				}
			}
		}
		raw = creative.JSON(fields)
	} else {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			return nil, nil, invalidWorkflow()
		}
		var err error
		raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			return nil, nil, invalidWorkflow()
		}
	}
	if commerce.Decode(raw, dst) != nil {
		return nil, nil, invalidWorkflow()
	}
	files := []map[string]string{}
	for _, u := range uploads {
		sum := sha256.Sum256(u.data)
		files = append(files, map[string]string{"sha256": hex.EncodeToString(sum[:]), "filename": u.name})
	}
	return uploads, creative.JSON(map[string]any{"input": dst, "uploads": files}), nil
}

func commerceUserText(value any) string {
	var raw any
	_ = json.Unmarshal(creative.JSON(value), &raw)
	var fields []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			fields = append(fields, x)
		case []any:
			for _, s := range x {
				walk(s)
			}
		case map[string]any:
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(x[k])
			}
		}
	}
	walk(raw)
	return strings.Join(fields, "\n")
}

func commerceDecode(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	raw := []byte(`{}`)
	if !optional || r.Body != nil && r.ContentLength != 0 {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			fail(w, r, 415, "JSON_REQUIRED", "Expected application/json")
			return false
		}
		var err error
		raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			fail(w, r, 400, "INVALID_REQUEST", "Invalid commerce request")
			return false
		}
	}
	if optional && (len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null") {
		raw = []byte(`{}`)
	}
	if commerce.Decode(raw, dst) != nil {
		fail(w, r, 400, "INVALID_REQUEST", "Invalid commerce request")
		return false
	}
	return true
}

func (s *Server) commerceReferences(ctx context.Context, user string, ids []string, uri string, uploads []creativeUpload) (refs []store.Asset, created []string, err error) {
	if len(ids)+len(uploads) > 10 {
		return nil, nil, invalidWorkflow()
	}
	refs = []store.Asset{}
	seen := map[string]bool{}
	var total int64
	add := func(a store.Asset) error {
		if seen[a.ID] {
			return nil
		}
		if a.Width <= 0 || a.Height <= 0 {
			return invalidWorkflow()
		}
		total += a.FileSize
		if total > 28<<20 || len(refs) >= 10 {
			return invalidWorkflow()
		}
		seen[a.ID] = true
		refs = append(refs, a)
		return nil
	}
	for _, id := range ids {
		a, e := s.Store.OwnedAsset(ctx, user, id)
		if e != nil {
			return refs, created, e
		}
		if e = add(a); e != nil {
			return refs, created, e
		}
	}
	if uri != "" {
		a, e := s.resolveCreativeReference(ctx, user, uri)
		if e != nil {
			return refs, created, e
		}
		if e = add(a); e != nil {
			return refs, created, e
		}
	}
	for _, u := range uploads {
		a, e := s.saveImage(ctx, user, "", "", "commerce-upload", u.name, bytes.NewReader(u.data))
		if e != nil {
			return refs, created, &workflowError{400, "INVALID_REFERENCE", "Use a valid JPEG, PNG or WebP within the image limits"}
		}
		created = append(created, a.ID)
		if e = add(a); e != nil {
			return refs, created, e
		}
	}
	return refs, created, nil
}

func (s *Server) discardCommerceUploads(user string, ids []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Store.DiscardCommerceUploads(ctx, user, ids); err != nil {
		s.Logger.Error("commerce input cleanup failed", "sqlstate", store.SQLState(err))
	}
}
