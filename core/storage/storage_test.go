package storage

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/security"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 30, 50))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(29, 49, color.RGBA{B: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestNormalizePreservesEntireImage(t *testing.T) {
	img, err := NormalizeImage(bytes.NewReader(fixture(t)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 30 || img.Height != 50 || img.ContentType != "image/png" {
		t.Fatal("image distorted")
	}
	decoded, _, err := image.Decode(bytes.NewReader(img.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.At(0, 0) != color.NRGBAModel.Convert(color.RGBA{R: 255, A: 255}) || decoded.At(29, 49) != color.NRGBAModel.Convert(color.RGBA{B: 255, A: 255}) {
		t.Fatal("corner pixels lost")
	}
}
func TestImageRejectsUnsupportedOrOversizedData(t *testing.T) {
	if _, err := NormalizeImage(bytes.NewReader([]byte(`<svg onload="alert(1)"></svg>`))); err == nil {
		t.Fatal("active content accepted")
	}
	if _, err := NormalizeImage(bytes.NewReader(bytes.Repeat([]byte{0}, int(MaxImageBytes+1)))); err != ErrImageTooLarge {
		t.Fatal("large image accepted")
	}
}
func TestLocalStorageCannotEscapeOrOverwrite(t *testing.T) {
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	img, _ := NormalizeImage(bytes.NewReader(fixture(t)))
	id := security.NewID("asset")
	key, err := s.WriteImage(id, img)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.Read(key, MaxImageBytes)
	if err != nil || !bytes.Equal(data, img.Bytes) {
		t.Fatal("stored bytes changed")
	}
	if _, err := s.WriteImage(id, img); err == nil {
		t.Fatal("existing asset overwritten")
	}
	for _, bad := range []string{"../outside", "/etc/passwd", "C:\\Windows\\win.ini", key + "/../../secret"} {
		if _, err := s.Open(bad); !os.IsNotExist(err) {
			t.Fatal("invalid key accepted")
		}
	}
}
func TestSignedAssetIsOwnerAndExpiryBound(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	now := time.Now()
	expires := now.Add(time.Minute).Unix()
	sig := SignAsset(key, "asset-test", "owner", expires)
	if !VerifyAsset(key, "asset-test", "owner", sig, expires, now) {
		t.Fatal("valid signature rejected")
	}
	if VerifyAsset(key, "asset-test", "other", sig, expires, now) || VerifyAsset(key, "asset-test", "owner", sig, expires, now.Add(2*time.Minute)) {
		t.Fatal("invalid signature accepted")
	}
}
