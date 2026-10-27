package storage

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

const objectStagingMarker = "~staging/"

// CleanupCandidates returns logical asset keys, never bucket-wide objects.
// Synthetic ~staging/ keys identify crash leftovers in this backend's private
// staging namespace, and are accepted only by RemoveCleanupCandidate. Both kinds
// still require the caller's database existence check. No deletion occurs here.
func (s *Object) CleanupCandidates(before time.Time, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 || before.IsZero() || before.After(time.Now()) {
		return nil, fs.ErrInvalid
	}
	if after != "" && !keyPattern.MatchString(after) && !validObjectStagingCandidate(after) {
		return nil, fs.ErrInvalid
	}
	ctx, cancel, err := s.operation(context.Background(), s.operationTimeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	keys := make([]string, 0, limit)
	if !strings.HasPrefix(after, objectStagingMarker) {
		start := ""
		if after != "" {
			start = s.prefix + after
		}
		fetchOwner := false
		options := minio.ListObjectsOptions{Prefix: s.prefix, Recursive: true, StartAfter: start, MaxKeys: 1000, FetchOwner: &fetchOwner}
		for info := range s.client.ListObjectsIter(ctx, s.bucket, options) {
			if info.Err != nil {
				return nil, objectError(info.Err)
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			key, ok := strings.CutPrefix(info.Key, s.prefix)
			if !ok || !keyPattern.MatchString(key) || key <= after || info.LastModified.IsZero() || !info.LastModified.Before(before) {
				continue
			}
			keys = append(keys, key)
			if len(keys) == limit {
				return keys, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stageAfter := strings.TrimPrefix(after, objectStagingMarker)
	if !strings.HasPrefix(after, objectStagingMarker) {
		stageAfter = ""
	}
	staged, err := s.staging.CleanupCandidates(before, stageAfter, limit-len(keys))
	if err != nil {
		return nil, objectError(err)
	}
	for _, key := range staged {
		keys = append(keys, objectStagingMarker+key)
	}
	return keys, nil
}

func validObjectStagingCandidate(key string) bool {
	key, ok := strings.CutPrefix(key, objectStagingMarker)
	return ok && (keyPattern.MatchString(key) || temporaryKeyPattern.MatchString(key))
}

func (s *Object) RemoveCleanupCandidate(key string, before time.Time) error {
	if before.IsZero() || before.After(time.Now()) {
		return fs.ErrInvalid
	}
	if s.ctx.Err() != nil {
		return fs.ErrClosed
	}
	if validObjectStagingCandidate(key) {
		// An active staging/upload operation cannot overlap old-file reclamation.
		// The normal maintenance grace is 48h, longer than the maximum 1h upload.
		if before.After(time.Now().Add(-time.Hour)) {
			return ErrObjectChanged
		}
		return objectError(s.staging.RemoveCleanupCandidate(strings.TrimPrefix(key, objectStagingMarker), before))
	}
	return s.remove(key, before)
}

// Remove is for an explicitly DB-checked asset deletion. It remains conditional
// on the observed object identity even when an age cutoff is not required.
func (s *Object) Remove(key string) error { return s.remove(key, time.Time{}) }

func (s *Object) remove(key string, before time.Time) error {
	if !keyPattern.MatchString(key) {
		return fs.ErrNotExist
	}
	ctx, cancel, err := s.operation(context.Background(), s.operationTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	info, err := s.client.StatObject(ctx, s.bucket, s.prefix+key, minio.StatObjectOptions{})
	if err != nil {
		return objectError(err)
	}
	if !before.IsZero() && (info.LastModified.IsZero() || !info.LastModified.Before(before)) {
		return ErrObjectChanged
	}
	if info.ETag == "" || len(info.ETag) > 256 || strings.ContainsAny(info.ETag, "\r\n\"\\") {
		return ErrObjectChanged
	}
	// SDK RemoveObject lacks conditional headers. SDK PresignHeader supplies the
	// same SigV4 signing for this DELETE; the URL is internal and never returned or
	// logged. Unsupported conditional deletion fails closed, with no fallback.
	headers := make(http.Header)
	headers.Set("If-Match", `"`+info.ETag+`"`)
	query := make(url.Values)
	if info.VersionID != "" {
		query.Set("versionId", info.VersionID)
	}
	signed, err := s.client.PresignHeader(ctx, http.MethodDelete, s.bucket, s.prefix+key, 5*time.Minute, query, headers)
	if err != nil {
		return objectError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, signed.String(), nil)
	if err != nil {
		return ErrObjectUnavailable
	}
	request.Header = headers
	response, err := s.http.Do(request)
	if err != nil {
		return objectError(err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return fs.ErrNotExist
	case http.StatusPreconditionFailed, http.StatusConflict:
		return ErrObjectChanged
	case http.StatusForbidden, http.StatusUnauthorized:
		return fs.ErrPermission
	default:
		return ErrObjectUnavailable
	}
}
