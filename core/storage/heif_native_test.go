package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Set PICPILOT_TEST_HEIF=1 in the native test stage. This is a required test,
// not a silent skip, when enabled. libheif's x265 encoder is test-only: fixtures
// are synthesized from colored pixels in t.TempDir; no photos or network I/O.
// The fixture encoder needs HEVC support; ExifTool adds the deterministic
// orientation field because the test fixture is synthetic and the container's
// libvips version does not preserve JPEG orientation during HEIF save.
func TestHEIFNativeSyntheticNormalization(t *testing.T) {
	if os.Getenv("PICPILOT_TEST_HEIF") != "1" {
		t.Skip("native HEIF integration is enabled with PICPILOT_TEST_HEIF=1")
	}
	vips, err := exec.LookPath("vips")
	if err != nil {
		t.Fatal("native integration requires libvips-tools")
	}
	header, err := exec.LookPath("vipsheader")
	if err != nil {
		t.Fatal("native integration requires vipsheader")
	}
	exiftool, err := exec.LookPath("exiftool")
	if err != nil {
		t.Fatal("native integration requires exiftool for deterministic HEIF orientation fixtures")
	}
	for _, orientation := range []byte{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Run(strconv.Itoa(int(orientation)), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "synthetic.jpg")
			heif := filepath.Join(dir, "synthetic.heic")
			if err := os.WriteFile(source, heifExif(heifFixtureJPEG(t, 64, 48), orientation), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// Native fixture generation is independent from the production converter.
			// Some encoders produce many NUMA warnings in containers; discard those
			// test-only diagnostics without persisting images or native output logs.
			command := HEIFCommand{Path: vips, Args: []string{"heifsave", source, heif, "--Q=95", "--compression=hevc", "--vips-concurrency=1"}, Dir: dir, Env: heifEnvironment(dir, vips, header), Stdout: io.Discard, Stderr: io.Discard}
			if err := (HEIFExecRunner{}).Run(ctx, command); err != nil {
				t.Fatal("synthetic fixture encoding failed; native test stage needs HEVC encoder support", err)
			}
			// Debian's libvips 8.14 does not copy JPEG EXIF orientation into
			// HEIF on save. Write the deterministic orientation tag with the
			// test-only ExifTool dependency so the converter is tested against
			// the same metadata real camera files carry.
			metadata := exec.CommandContext(ctx, exiftool, "-overwrite_original", fmt.Sprintf("-Orientation#=%d", orientation), heif)
			metadata.Dir = dir
			if output, err := metadata.CombinedOutput(); err != nil {
				t.Fatalf("synthetic HEIF orientation metadata failed: %v (%s)", err, bytes.TrimSpace(output))
			}
			raw, err := os.ReadFile(heif)
			if err != nil || !IsHEIF(raw) {
				t.Fatal("synthetic encoder did not produce a HEIF file", err)
			}
			temp := t.TempDir()
			converter, err := NewHEIFConverter(HEIFOptions{TempDir: temp, VIPSPath: vips, VIPSHeaderPath: header})
			if err != nil {
				t.Fatal(err)
			}
			result, err := converter.Normalize(ctx, bytes.NewReader(raw))
			if err != nil {
				t.Fatal("native HEIF normalization failed", err)
			}
			wantW, wantH := 64, 48
			if orientation >= 5 {
				wantW, wantH = wantH, wantW
			}
			if result.Width != wantW || result.Height != wantH || result.ContentType != "image/jpeg" || bytes.Contains(result.Bytes, []byte("Exif")) {
				t.Fatal("native conversion did not preserve orientation/dimensions or strip metadata", result.Width, result.Height)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(result.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			palette := []color.RGBA{{R: 240, A: 255}, {G: 240, A: 255}, {B: 240, A: 255}, {R: 240, G: 240, A: 255}}
			quadrants := [8][4]int{{0, 1, 2, 3}, {1, 0, 3, 2}, {3, 2, 1, 0}, {2, 3, 0, 1}, {0, 2, 1, 3}, {2, 0, 3, 1}, {3, 1, 2, 0}, {1, 3, 0, 2}}
			for i, point := range [][2]int{{wantW / 4, wantH / 4}, {3 * wantW / 4, wantH / 4}, {wantW / 4, 3 * wantH / 4}, {3 * wantW / 4, 3 * wantH / 4}} {
				got := color.RGBAModel.Convert(decoded.At(point[0], point[1])).(color.RGBA)
				want := palette[quadrants[orientation-1][i]]
				for _, pair := range [][2]byte{{got.R, want.R}, {got.G, want.G}, {got.B, want.B}} {
					if delta := int(pair[0]) - int(pair[1]); delta < -45 || delta > 45 {
						t.Fatal("synthetic quadrants were cropped, mirrored or rotated incorrectly")
					}
				}
			}
			if _, err := converter.Normalize(ctx, bytes.NewReader(heifHeader("heic", "mif1"))); !errors.Is(err, ErrInvalidImage) {
				t.Fatal("native corrupt-input rejection failed", err)
			}
			remaining, err := os.ReadDir(temp)
			if err != nil || len(remaining) != 0 {
				t.Fatal("native conversion leaked temporary files", err)
			}
		})
	}
}
