package storage

import (
	"errors"
	"io"
	"os"
	"path"
	"regexp"

	"github.com/suakitsu/picpilot-platform-go/core/security"
)

type Local struct{ root *os.Root }

var keyPattern = regexp.MustCompile(`^[a-f0-9]{2}/asset_[a-f0-9]{32}\.(png|jpg|webp|gif|mp4|webm|mp3|wav|json)$`)

func NewLocal(directory string) (*Local, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, errors.New("cannot create asset directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.New("cannot open asset directory")
	}
	return &Local{root: root}, nil
}
func (s *Local) Close() error { return s.root.Close() }
func (s *Local) WriteImage(id string, img Image) (string, error) {
	if len(img.SHA256) != 64 {
		return "", ErrInvalidImage
	}
	key := img.SHA256[:2] + "/" + id + "." + img.Extension
	if !keyPattern.MatchString(key) {
		return "", errors.New("invalid storage key")
	}
	if err := s.root.MkdirAll(path.Dir(key), 0700); err != nil {
		return "", errors.New("cannot create asset directory")
	}
	temp := security.NewID("tmp")
	f, err := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errors.New("cannot create asset file")
	}
	defer s.root.Remove(temp)
	if _, err = f.Write(img.Bytes); err != nil {
		f.Close()
		return "", errors.New("asset write failed")
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return "", errors.New("asset sync failed")
	}
	if err = f.Close(); err != nil {
		return "", errors.New("asset close failed")
	}
	if err = s.root.Link(temp, key); err != nil {
		return "", errors.New("asset commit failed")
	}
	return key, nil
}
func (s *Local) Open(key string) (*os.File, error) {
	if !keyPattern.MatchString(key) {
		return nil, os.ErrNotExist
	}
	return s.root.Open(key)
}
func (s *Local) Read(key string, max int64) ([]byte, error) {
	f, err := s.Open(key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if max < 1 || max > 1<<30 {
		return nil, errors.New("invalid read limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, errors.New("asset read failed")
	}
	return data, nil
}
func (s *Local) Remove(key string) error {
	if !keyPattern.MatchString(key) {
		return os.ErrNotExist
	}
	return s.root.Remove(key)
}
