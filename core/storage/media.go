package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/suakitsu/picpilot-platform/core/security"
)

const MaxMediaBytes int64 = 500 << 20

var ErrInvalidMedia = errors.New("unsupported or invalid media")
var ErrMediaTooLarge = errors.New("media exceeds limit")

type Media struct {
	StorageKey  string
	SHA256      string
	ContentType string
	Extension   string
	FileSize    int64
}

// WriteMedia streams large, non-executable media without retaining it in RAM.
// Container signatures, rather than a filename or upstream header, select MIME.
func (s *Local) WriteMedia(id string, reader io.Reader, expectedMIME string) (Media, error) {
	var head [512]byte
	n, err := io.ReadFull(reader, head[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Media{}, ErrInvalidMedia
	}
	if n == 0 {
		return Media{}, ErrInvalidMedia
	}
	mime := http.DetectContentType(head[:n])
	ext := ""
	switch mime {
	case "video/mp4":
		ext = "mp4"
	case "video/webm":
		ext = "webm"
	case "audio/mpeg":
		ext = "mp3"
	case "audio/wave", "audio/x-wav", "audio/wav":
		mime = "audio/wav"
		ext = "wav"
	case "image/gif":
		ext = "gif"
	default:
		return Media{}, ErrInvalidMedia
	}
	if expectedMIME != "" && expectedMIME != "application/octet-stream" && expectedMIME != mime {
		return Media{}, ErrInvalidMedia
	}
	if !keyPattern.MatchString("00/" + id + "." + ext) {
		return Media{}, ErrInvalidMedia
	}
	temp := security.NewID("tmp")
	f, err := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Media{}, errors.New("cannot create media file")
	}
	defer s.root.Remove(temp)
	hash := sha256.New()
	w := io.MultiWriter(f, hash)
	if _, err = w.Write(head[:n]); err != nil {
		f.Close()
		return Media{}, errors.New("media write failed")
	}
	copied, err := io.Copy(w, io.LimitReader(reader, MaxMediaBytes-int64(n)+1))
	if err != nil {
		f.Close()
		return Media{}, errors.New("media read failed")
	}
	size := copied + int64(n)
	if size > MaxMediaBytes {
		f.Close()
		return Media{}, ErrMediaTooLarge
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return Media{}, errors.New("media sync failed")
	}
	if err = f.Close(); err != nil {
		return Media{}, errors.New("media close failed")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	key := digest[:2] + "/" + id + "." + ext
	if err = s.root.MkdirAll(path.Dir(key), 0700); err != nil {
		return Media{}, errors.New("cannot create media directory")
	}
	if err = s.root.Link(temp, key); err != nil {
		return Media{}, errors.New("media commit failed")
	}
	return Media{StorageKey: key, SHA256: digest, ContentType: mime, Extension: ext, FileSize: size}, nil
}

func SafeFilename(filename, fallback string) string {
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)
	if len(filename) > 200 || filename == "" || filename == "." || filename == "/" {
		return fallback
	}
	return filename
}
