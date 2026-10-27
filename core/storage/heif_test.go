package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type heifRunnerFunc func(context.Context, HEIFCommand) error

func (f heifRunnerFunc) Run(ctx context.Context, c HEIFCommand) error { return f(ctx, c) }

func heifHeader(major string, compatible ...string) []byte {
	raw := make([]byte, 16+4*len(compatible))
	binary.BigEndian.PutUint32(raw, uint32(len(raw)))
	copy(raw[4:], "ftyp")
	copy(raw[8:], major)
	for i, brand := range compatible {
		copy(raw[16+i*4:], brand)
	}
	return raw
}

func heifOrientationFixture(little bool, orientation byte) []byte {
	raw := make([]byte, 8+2+12+4)
	var order binary.ByteOrder = binary.BigEndian
	copy(raw[:2], "MM")
	if little {
		order = binary.LittleEndian
		copy(raw[:2], "II")
	}
	order.PutUint16(raw[2:4], 42)
	order.PutUint32(raw[4:8], 8)
	order.PutUint16(raw[8:10], 1)
	order.PutUint16(raw[10:12], 0x0112)
	order.PutUint16(raw[12:14], 3)
	order.PutUint32(raw[14:18], 1)
	order.PutUint16(raw[18:20], uint16(orientation))
	return append([]byte("fixture"), append([]byte{'E', 'x', 'i', 'f', 0, 0}, raw...)...)
}

func TestHEIFOrientationParser(t *testing.T) {
	for _, little := range []bool{false, true} {
		for orientation := byte(1); orientation <= 8; orientation++ {
			if got := heifOrientation(heifOrientationFixture(little, orientation)); got != orientation {
				t.Fatalf("little=%v orientation=%d parsed as %d", little, orientation, got)
			}
		}
	}
	if got := heifOrientation(heifOrientationFixture(true, 9)); got != 1 {
		t.Fatalf("invalid orientation parsed as %d", got)
	}
	if got := heifOrientation([]byte("Exif\\x00\\x00bad")); got != 1 {
		t.Fatalf("malformed Exif parsed as %d", got)
	}
}

// Entirely synthetic: four solid quadrants make rotation/crop mistakes visible.
func heifFixtureJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	palette := []color.RGBA{{R: 240, A: 255}, {G: 240, A: 255}, {B: 240, A: 255}, {R: 240, G: 240, A: 255}}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := 0
			if x >= width/2 {
				index++
			}
			if y >= height/2 {
				index += 2
			}
			img.SetRGBA(x, y, palette[index])
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 99}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func heifExif(raw []byte, orientation byte) []byte {
	// One TIFF orientation field, generated locally rather than copied metadata.
	data := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, orientation, 0, 0, 0, 0, 0, 0, 0}
	segment := []byte{0xff, 0xe1, 0, byte(len(data) + 2)}
	out := append([]byte{}, raw[:2]...)
	out = append(out, segment...)
	out = append(out, data...)
	return append(out, raw[2:]...)
}

func heifMock(t *testing.T, run HEIFRunner) *HEIFConverter {
	t.Helper()
	dir := t.TempDir()
	c, err := NewHEIFConverter(HEIFOptions{Runner: run, TempDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 0 {
			t.Error("HEIF conversion left temporary files", err)
		}
	})
	return c
}

func heifReply(c HEIFCommand, width, height string, jpeg []byte) error {
	if len(c.Args) >= 2 && c.Args[0] == "-f" {
		value := width
		if c.Args[1] == "height" {
			value = height
		}
		_, err := io.WriteString(c.Stdout, value+"\n")
		return err
	}
	_, err := c.Stdout.Write(jpeg)
	return err
}

func TestHEIFSignatureBoundsAndOtherFormats(t *testing.T) {
	extended := append([]byte{0, 0, 0, 1, 'f', 't', 'y', 'p', 0, 0, 0, 0, 0, 0, 0, 28}, heifHeader("heic", "mif1")[8:]...)
	leading := append([]byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}, heifHeader("mif1", "heic")...)
	minor := heifHeader("isom")
	copy(minor[12:], "heic")
	for _, test := range []struct {
		name string
		raw  []byte
		want bool
	}{
		{"heic", heifHeader("heic", "mif1"), true},
		{"heif", heifHeader("mif1", "heix"), true},
		{"sequence", heifHeader("msf1", "hevc"), true},
		{"extended", extended, true}, {"free", leading, true},
		{"avif", heifHeader("avif", "mif1"), false},
		{"mixed-avif", heifHeader("heic", "avif"), false},
		{"mp4", heifHeader("isom", "mp41"), false}, {"minor-version", minor, false},
		{"fake", []byte("not a photo.heic"), false},
		{"truncated", heifHeader("heic")[:15], false},
		{"zero-size", []byte{0, 0, 0, 0, 'f', 't', 'y', 'p'}, false},
		{"huge-box", []byte{255, 255, 255, 255, 'f', 't', 'y', 'p'}, false},
		{"short-extended", []byte{0, 0, 0, 1, 'f', 't', 'y', 'p'}, false},
		{"unknown-leading", append([]byte{0, 0, 0, 8, 'm', 'd', 'a', 't'}, heifHeader("heic")...), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsHEIF(test.raw); got != test.want {
				t.Fatalf("classification %v, want %v", got, test.want)
			}
		})
	}
}

func TestHEIFRejectsDimensionsBeforePixelDecode(t *testing.T) {
	for _, test := range []struct{ width, height string }{
		{"0", "1"}, {"-1", "2"}, {"18446744073709551616", "2"},
		{"50000001", "1"}, {"50000000", "2"}, {"10000", "5001"},
		{"12", "99999999999999999999"}, {"12", "NaN"}, {"1e6", "1"},
		{"12", strings.Repeat("1", 129)}, {"12\nprivate diagnostic", "20"},
	} {
		t.Run(test.width+"x"+test.height[:min(12, len(test.height))], func(t *testing.T) {
			decoded := false
			c := heifMock(t, heifRunnerFunc(func(ctx context.Context, cmd HEIFCommand) error {
				if cmd.Args[0] != "-f" {
					decoded = true
				}
				return heifReply(cmd, test.width, test.height, nil)
			}))
			_, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic")))
			if err == nil || decoded || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid dimensions reached conversion or leaked diagnostics")
			}
		})
	}
}

func TestHEIFNormalizeRotationHashesMetadataAndEnvironment(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		t.Run(fmt.Sprint(rotated), func(t *testing.T) {
			w, h := 48, 32
			if rotated {
				w, h = h, w
			}
			output := heifExif(heifFixtureJPEG(t, w, h), 1)
			source := heifHeader("heic", "mif1")
			calls := 0
			t.Setenv("PICPILOT_HEIF_TEST_SECRET", "fixture-only-value")
			t.Setenv("LIBHEIF_SECURITY_LIMITS", "off")
			c := heifMock(t, heifRunnerFunc(func(ctx context.Context, cmd HEIFCommand) error {
				calls++
				if err := ctx.Err(); err != nil {
					return err
				}
				for _, env := range cmd.Env {
					if strings.Contains(env, "fixture-only") || strings.HasPrefix(env, "LIBHEIF_SECURITY_LIMITS=") {
						t.Error("child inherited unsafe environment")
					}
				}
				input, err := os.ReadFile(filepath.Join(cmd.Dir, "source.heic"))
				if err != nil || !bytes.Equal(input, source) {
					t.Error("native input was not the exact bounded source")
				}
				if runtime.GOOS != "windows" {
					info, _ := os.Stat(cmd.Dir)
					if info.Mode().Perm() != 0700 {
						t.Error("temporary directory is not private")
					}
				}
				if calls == 1 && cmd.Args[1] != "width" || calls == 2 && cmd.Args[1] != "height" {
					t.Error("conversion preceded metadata bounds")
				}
				if calls == 3 && (cmd.Args[0] != "copy" || cmd.Args[2] != ".jpg[Q=92,strip,optimize_coding]") {
					t.Error("unexpected transformation or unbounded output file")
				}
				return heifReply(cmd, "48", "32", output)
			}))
			result, err := c.Normalize(context.Background(), bytes.NewReader(source))
			if err != nil || calls != 3 || result.Width != w || result.Height != h || result.ContentType != "image/jpeg" || result.Extension != "jpg" {
				t.Fatal("normalization or rotated dimensions failed", err)
			}
			if bytes.Contains(result.Bytes, []byte("Exif")) {
				t.Fatal("EXIF metadata survived normalization")
			}
			original, normalized := sha256.Sum256(source), sha256.Sum256(result.Bytes)
			if result.SourceSHA256 != hex.EncodeToString(original[:]) || result.SHA256 != hex.EncodeToString(normalized[:]) {
				t.Fatal("source/output digests were mixed")
			}
			if _, err := jpeg.Decode(bytes.NewReader(result.Bytes)); err != nil {
				t.Fatal("result is not a complete JPEG")
			}
		})
	}
}

func TestHEIFInputAndNativeOutputFailures(t *testing.T) {
	good := heifFixtureJPEG(t, 32, 48)
	large := append([]byte{}, good...)
	sof := bytes.Index(large, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("synthetic JPEG is missing its frame header")
	}
	binary.BigEndian.PutUint16(large[sof+5:sof+7], 10000)
	binary.BigEndian.PutUint16(large[sof+7:sof+9], 10000)
	if config, err := jpeg.DecodeConfig(bytes.NewReader(large)); err != nil || config.Width != 10000 || config.Height != 10000 {
		t.Fatal("synthetic oversized JPEG header is invalid", err)
	}
	for _, test := range []struct {
		name string
		out  []byte
	}{
		{"invalid", []byte("native output is not a jpeg")},
		{"truncated", good[:len(good)/2]},
		{"cropped", heifFixtureJPEG(t, 32, 24)},
		{"resized", heifFixtureJPEG(t, 16, 24)},
		{"oversized-header", large},
		{"oversized", bytes.Repeat([]byte{1}, int(MaxImageBytes+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := heifMock(t, heifRunnerFunc(func(_ context.Context, cmd HEIFCommand) error {
				return heifReply(cmd, "32", "48", test.out)
			}))
			if _, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic"))); err == nil {
				t.Fatal("invalid or dimension-changing conversion accepted")
			} else if test.name == "oversized-header" && !errors.Is(err, ErrImageTooLarge) {
				t.Fatal("native JPEG dimensions were not checked before pixel decode", err)
			}
		})
	}
	for _, input := range [][]byte{[]byte("corrupt"), append(heifHeader("heic"), make([]byte, MaxImageBytes)...)} {
		c := heifMock(t, heifRunnerFunc(func(context.Context, HEIFCommand) error {
			t.Error("invalid/oversized input reached a native process")
			return nil
		}))
		if _, err := c.Normalize(context.Background(), bytes.NewReader(input)); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	for _, phase := range []string{"width", "height", "copy"} {
		t.Run(phase, func(t *testing.T) {
			c := heifMock(t, heifRunnerFunc(func(_ context.Context, cmd HEIFCommand) error {
				if cmd.Args[0] == phase || len(cmd.Args) > 1 && cmd.Args[1] == phase {
					_, _ = io.WriteString(cmd.Stderr, "internal path and fixture credentials must not escape")
					return errors.New("private native process diagnostics")
				}
				return heifReply(cmd, "32", "48", good)
			}))
			if _, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic"))); !errors.Is(err, ErrInvalidImage) {
				t.Fatal("native failure was not safely normalized", err)
			}
		})
	}
}

func TestHEIFPassthroughUsesExistingRasterPolicy(t *testing.T) {
	c := heifMock(t, heifRunnerFunc(func(context.Context, HEIFCommand) error {
		t.Error("raster input should not invoke libvips")
		return nil
	}))
	var pngFixture bytes.Buffer
	if err := png.Encode(&pngFixture, image.NewRGBA(image.Rect(0, 0, 32, 48))); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{pngFixture.Bytes(), heifExif(heifFixtureJPEG(t, 32, 48), 6)} {
		want, err := NormalizeImage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Normalize(context.Background(), bytes.NewReader(raw))
		if err != nil || got.SHA256 != want.SHA256 || got.SourceSHA256 != want.SourceSHA256 || got.Width != want.Width || got.Height != want.Height {
			t.Fatal("existing raster normalization changed", err)
		}
	}
}

func TestHEIFCancelledQueueDoesNotUseOrLeakSlot(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var calls atomic.Int32
	output := heifFixtureJPEG(t, 32, 48)
	c := heifMock(t, heifRunnerFunc(func(ctx context.Context, cmd HEIFCommand) error {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return heifReply(cmd, "32", "48", output)
	}))
	go func() {
		_, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic")))
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Normalize(ctx, bytes.NewReader(heifHeader("heic")))
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 || len(c.slots) != 1 {
		t.Error("cancelled queued caller touched a process or consumed the active slot")
	}
	queued, cancelQueued := context.WithCancel(context.Background())
	timer := time.AfterFunc(25*time.Millisecond, cancelQueued)
	_, err = c.Normalize(queued, bytes.NewReader(heifHeader("heic")))
	timer.Stop()
	cancelQueued()
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 || len(c.slots) != 1 {
		t.Error("cancellation while waiting touched a process or leaked a slot")
	}
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, err = c.Normalize(deadline, bytes.NewReader(heifHeader("heic")))
	cancelDeadline()
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 || len(c.slots) != 1 {
		t.Error("queue wait was not included in the deadline")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic"))); err != nil || calls.Load() != 6 || len(c.slots) != 0 {
		t.Fatal("cancelled queued caller leaked a slot", err)
	}
}

func TestHEIFDeadlineAndBoundedDiagnostics(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		c := heifMock(t, heifRunnerFunc(func(ctx context.Context, _ HEIFCommand) error {
			<-ctx.Done()
			return ctx.Err()
		}))
		c.timeout = 20 * time.Millisecond
		if _, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic"))); !errors.Is(err, context.DeadlineExceeded) || len(c.slots) != 0 {
			t.Fatal("deadline not propagated or slot leaked", err)
		}
	})
	t.Run("stderr", func(t *testing.T) {
		c := heifMock(t, heifRunnerFunc(func(ctx context.Context, cmd HEIFCommand) error {
			_, err := io.Copy(cmd.Stderr, strings.NewReader(strings.Repeat("diagnostic", 1000)))
			if err == nil || ctx.Err() == nil || cmd.Stderr.(*heifBuffer).Len() != 4096 {
				t.Error("stderr cap did not cancel the process or was bypassed by io.Copy")
			}
			return err
		}))
		if _, err := c.Normalize(context.Background(), bytes.NewReader(heifHeader("heic"))); !errors.Is(err, ErrInvalidImage) {
			t.Fatal("oversized native diagnostic was exposed", err)
		}
	})
}

func TestHEIFOptionsAndMissingBinary(t *testing.T) {
	for _, o := range []HEIFOptions{{Timeout: -1}, {Timeout: 31 * time.Second}, {MaxConcurrent: -1}, {MaxConcurrent: 5}, {VIPSPath: "./untrusted-vips"}, {TempDir: filepath.Join(t.TempDir(), "missing")}} {
		o.Runner = heifRunnerFunc(func(context.Context, HEIFCommand) error { return nil })
		if _, err := NewHEIFConverter(o); !errors.Is(err, ErrHEIFOptions) {
			t.Fatal("invalid HEIF options accepted", err)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing-executable")
	if _, err := NewHEIFConverter(HEIFOptions{VIPSPath: missing}); !errors.Is(err, ErrHEIFUnavailable) || strings.Contains(err.Error(), missing) {
		t.Fatal("missing binary did not return a safe availability error", err)
	}
}

func TestHEIFExecRunnerCancellationAndOutputLimit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wait", "stderr", "stdout"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			out, diagnostics := &heifBuffer{limit: 128, cancel: cancel}, &heifBuffer{limit: 4096, cancel: cancel}
			command := HEIFCommand{Path: self, Args: []string{"-test.run=^TestHEIFHelperProcess$", "--", "heif-helper-" + mode}, Dir: t.TempDir(), Stdout: out, Stderr: diagnostics}
			command.Env = heifEnvironment(command.Dir, self, self)
			start := time.Now()
			err := (HEIFExecRunner{}).Run(ctx, command)
			if ctx.Err() == nil || err == nil || time.Since(start) > 5*time.Second || diagnostics.Len() > 4096 || out.Len() > 128 {
				t.Fatal("native child not cancelled/joined within bounds", err)
			}
			if mode == "stderr" && !diagnostics.exceeded {
				t.Fatal("native stderr writer did not enforce the cap")
			}
			if mode == "stdout" && !out.exceeded {
				t.Fatal("native stdout writer did not enforce the cap")
			}
		})
	}
}

func TestHEIFHelperProcess(t *testing.T) {
	mode := os.Args[len(os.Args)-1]
	if !strings.HasPrefix(mode, "heif-helper-") {
		return
	}
	if mode == "heif-helper-stderr" {
		_, _ = io.WriteString(os.Stderr, strings.Repeat("synthetic diagnostic\n", 10000))
	}
	if mode == "heif-helper-stdout" {
		_, _ = io.WriteString(os.Stdout, strings.Repeat("synthetic output\n", 10000))
	}
	time.Sleep(10 * time.Second)
	os.Exit(0)
}
