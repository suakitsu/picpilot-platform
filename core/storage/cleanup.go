package storage

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"time"
)

var temporaryKeyPattern = regexp.MustCompile(`^tmp_[a-f0-9]{32}$`)

// CleanupCandidates only lists regular files in the application's os.Root.
// It never follows symlinks and is not a recursive filesystem deletion API.
func (s *Local) CleanupCandidates(before time.Time, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("invalid cleanup limit")
	}
	out := []string{}
	err := fs.WalkDir(s.root.FS(), ".", func(key string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if key <= after || !entry.Type().IsRegular() || (!keyPattern.MatchString(key) && !temporaryKeyPattern.MatchString(key)) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(before) {
			out = append(out, key)
		}
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return out, err
}

func (s *Local) RemoveCleanupCandidate(key string, before time.Time) error {
	if !keyPattern.MatchString(key) && !temporaryKeyPattern.MatchString(key) {
		return os.ErrNotExist
	}
	info, err := s.root.Lstat(key)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !info.ModTime().Before(before) {
		return errors.New("cleanup candidate changed")
	}
	return s.root.Remove(key)
}
