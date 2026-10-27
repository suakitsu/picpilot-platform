package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func TestMediaUsesSignatureAndPreservesBytes(t *testing.T) {
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A small ISO BMFF ftyp header; no real video or personal media fixture.
	data := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	media, err := s.WriteMedia(security.NewID("asset"), bytes.NewReader(data), "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(media.StorageKey, 1024)
	digest := sha256.Sum256(data)
	if err != nil || !bytes.Equal(got, data) || media.FileSize != int64(len(data)) || media.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("streamed media changed")
	}
	if _, err = s.WriteMedia(security.NewID("asset"), bytes.NewReader(data), "video/webm"); !errors.Is(err, ErrInvalidMedia) {
		t.Fatal("incorrect MIME accepted")
	}
	if _, err = s.WriteMedia(security.NewID("asset"), bytes.NewBufferString("<script>alert(1)</script>"), "video/mp4"); !errors.Is(err, ErrInvalidMedia) {
		t.Fatal("script disguised as video accepted")
	}
	if _, err = s.WriteMedia("../../outside", bytes.NewReader(data), "video/mp4"); !errors.Is(err, ErrInvalidMedia) {
		t.Fatal("unsafe media path accepted")
	}
}
