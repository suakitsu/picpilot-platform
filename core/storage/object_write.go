package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"io/fs"

	"github.com/minio/minio-go/v7"
)

func (s *Object) WriteImage(id string, img Image) (string, error) {
	return s.WriteImageContext(context.Background(), id, img)
}

func (s *Object) WriteImageContext(parent context.Context, id string, img Image) (string, error) {
	ctx, cancel, err := s.operation(parent, s.uploadTimeout)
	if err != nil {
		return "", err
	}
	defer cancel()
	if err := s.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-s.slots }()
	if int64(len(img.Bytes)) > MaxImageBytes {
		return "", ErrImageTooLarge
	}
	if len(img.Bytes) == 0 {
		return "", ErrInvalidImage
	}
	sum := sha256.Sum256(img.Bytes)
	if img.SHA256 != hex.EncodeToString(sum[:]) {
		return "", ErrInvalidImage
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(img.Bytes))
	if err != nil || !allowedFormat(format) {
		return "", ErrInvalidImage
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width) > MaxImagePixels/int64(config.Height) {
		return "", ErrImageTooLarge
	}
	extension, contentType := format, "image/"+format
	if format == "jpeg" {
		extension = "jpg"
	}
	if img.Extension != extension || img.ContentType != contentType || img.Width != config.Width || img.Height != config.Height {
		return "", ErrInvalidImage
	}
	key := img.SHA256[:2] + "/" + id + "." + img.Extension
	if !keyPattern.MatchString(key) {
		return "", ErrInvalidImage
	}
	if err := s.put(ctx, key, bytes.NewReader(img.Bytes), int64(len(img.Bytes)), img.ContentType, img.SHA256); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Object) WriteMedia(id string, reader io.Reader, expectedMIME string) (Media, error) {
	return s.WriteMediaContext(context.Background(), id, reader, expectedMIME)
}

// WriteMediaContext reuses the local signature validation and 500 MiB streaming
// bound. Disk staging gives the SDK a seekable, known-length stream, so checksums
// and a single conditional PUT never allocate a video-sized memory buffer or
// leave multipart uploads behind. Failed/ambiguous uploads are not auto-deleted.
func (s *Object) WriteMediaContext(parent context.Context, id string, reader io.Reader, expectedMIME string) (Media, error) {
	ctx, cancel, err := s.operation(parent, s.uploadTimeout)
	if err != nil {
		return Media{}, err
	}
	defer cancel()
	if reader == nil {
		return Media{}, ErrInvalidMedia
	}
	if err := s.acquire(ctx); err != nil {
		return Media{}, err
	}
	defer func() { <-s.slots }()
	media, err := s.staging.WriteMedia(id, objectContextReader{ctx, reader}, expectedMIME)
	if err != nil {
		if ctx.Err() != nil {
			return Media{}, ctx.Err()
		}
		return Media{}, objectError(err)
	}
	defer s.staging.Remove(media.StorageKey)
	file, err := s.staging.Open(media.StorageKey)
	if err != nil {
		return Media{}, objectError(err)
	}
	defer file.Close()
	if err := s.put(ctx, media.StorageKey, file, media.FileSize, media.ContentType, media.SHA256); err != nil {
		return Media{}, err
	}
	return media, nil
}

func (s *Object) put(ctx context.Context, key string, reader io.Reader, size int64, contentType, sha string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	options := minio.PutObjectOptions{
		ContentType:      contentType,
		UserMetadata:     map[string]string{"picpilot-sha256": sha},
		DisableMultipart: true, DisableContentSha256: true, SendContentMd5: true,
	}
	// bytes.Reader and os.File implement ReaderAt/Seeker: the SDK's MD5 pass is
	// streaming, not its fallback allocation for a non-seekable reader.
	options.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, reader, size, options)
	err = objectError(err)
	if errors.Is(err, ErrObjectChanged) {
		return fs.ErrExist
	}
	return err
}

type objectContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r objectContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
