package storage

import (
	"io"
	"io/fs"
	"time"
)

// File supports both http.ServeContent and media callers that inspect its size.
type File interface {
	io.ReadSeekCloser
	Stat() (fs.FileInfo, error)
}

// Files owns bytes only. Callers must authorize reads and check database asset
// references before Remove or RemoveCleanupCandidate. Keys are not public URLs.
type Files interface {
	Open(string) (File, error)
	Read(string, int64) ([]byte, error)
	WriteImage(string, Image) (string, error)
	WriteMedia(string, io.Reader, string) (Media, error)
	Remove(string) error
	CleanupCandidates(time.Time, string, int) ([]string, error)
	RemoveCleanupCandidate(string, time.Time) error
	Close() error
}

type localFiles struct{ *Local }

// WrapLocal adapts Local's concrete *os.File return without changing local
// storage behavior or requiring any object-storage service for quickstart.
func WrapLocal(local *Local) Files { return &localFiles{local} }

func (s *localFiles) Open(key string) (File, error) { return s.Local.Open(key) }

var _ Files = (*localFiles)(nil)
