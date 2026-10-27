package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

func (s *Object) Open(key string) (File, error) { return s.OpenContext(context.Background(), key) }

// OpenContext returns a bounded, seekable range reader. The SDK pins subsequent
// GET/range requests to the ETag observed by Stat, so Seek cannot mix versions.
// The caller must Close it; its context and ReadTimeout cover its entire life.
func (s *Object) OpenContext(parent context.Context, key string) (File, error) {
	if !keyPattern.MatchString(key) {
		return nil, fs.ErrNotExist
	}
	ctx, cancel, err := s.operation(parent, s.readTimeout)
	if err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, objectError(err)
	}
	info, err := object.Stat()
	if err != nil || info.Size < 0 || info.Size > MaxMediaBytes || info.ETag == "" {
		object.Close()
		cancel()
		if err != nil {
			return nil, objectError(err)
		}
		if info.Size > MaxMediaBytes {
			return nil, ErrMediaTooLarge
		}
		return nil, ErrObjectUnavailable
	}
	return &objectFile{object: object, ctx: ctx, cancel: cancel, info: objectFileInfo{name: path.Base(key), size: info.Size, modified: info.LastModified}}, nil
}

// Read is intentionally limited to image-sized buffers. Large media must use
// Open/Seek; passing a larger limit never allocates a video-sized byte slice.
func (s *Object) Read(key string, max int64) ([]byte, error) {
	return s.ReadContext(context.Background(), key, max)
}

func (s *Object) ReadContext(ctx context.Context, key string, max int64) ([]byte, error) {
	if max < 1 || max > MaxImageBytes {
		return nil, ErrObjectReadLimit
	}
	file, err := s.OpenContext(ctx, key)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > max {
		return nil, ErrObjectReadLimit
	}
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, objectError(err)
	}
	if int64(len(data)) != info.Size() || int64(len(data)) > max {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

type objectFile struct {
	mu     sync.Mutex
	object *minio.Object
	ctx    context.Context
	cancel context.CancelFunc
	info   objectFileInfo
	offset int64
	closed bool
}

func (f *objectFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fs.ErrClosed
	}
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if f.offset >= f.info.size {
		return 0, io.EOF
	}
	if remaining := f.info.size - f.offset; int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := f.object.Read(p)
	f.offset += int64(n)
	if errors.Is(err, io.EOF) && f.offset < f.info.size {
		return n, io.ErrUnexpectedEOF
	}
	return n, objectError(err)
}

func (f *objectFile) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fs.ErrClosed
	}
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	position, err := f.object.Seek(offset, whence)
	if err != nil {
		return 0, objectError(err)
	}
	f.offset = position
	return position, nil
}

func (f *objectFile) Stat() (fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, fs.ErrClosed
	}
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	return f.info, nil
}

func (f *objectFile) Close() error {
	f.cancel()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return objectError(f.object.Close())
}

type objectFileInfo struct {
	name     string
	size     int64
	modified time.Time
}

func (i objectFileInfo) Name() string       { return i.name }
func (i objectFileInfo) Size() int64        { return i.size }
func (i objectFileInfo) Mode() fs.FileMode  { return 0600 }
func (i objectFileInfo) ModTime() time.Time { return i.modified }
func (i objectFileInfo) IsDir() bool        { return false }
func (i objectFileInfo) Sys() any           { return nil }

var _ File = (*objectFile)(nil)
