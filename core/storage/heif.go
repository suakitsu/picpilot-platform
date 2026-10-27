package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

var ErrHEIFUnavailable = errors.New("HEIF conversion is unavailable")
var ErrHEIFOptions = errors.New("invalid HEIF converter options")

// HEIFCommand is an internal native invocation, never a user-supplied command.
// Runners must honor Context, join their process/output writers before returning,
// and never log command output, image bytes, paths, or inherited credentials.
type HEIFCommand struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

type HEIFRunner interface {
	Run(context.Context, HEIFCommand) error
}

type HEIFOptions struct {
	VIPSPath       string
	VIPSHeaderPath string
	TempDir        string
	Timeout        time.Duration // Defaults to 30 seconds; may only be reduced.
	MaxConcurrent  int           // Defaults to one; range 1-4.
	Runner         HEIFRunner    // Nil uses direct exec, without a shell.
}

// HEIFConverter supplements NormalizeImage; it does not register a global image
// decoder or change which formats the Worker artifact protocol permits. Reuse
// one instance per server so its concurrency limit applies across requests.
// Native memory/CPU/PID/temp-disk hard limits remain a deployment responsibility.
type HEIFConverter struct {
	vips, header, temp string
	timeout            time.Duration
	runner             HEIFRunner
	slots              chan struct{}
}

func NewHEIFConverter(o HEIFOptions) (*HEIFConverter, error) {
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	if o.MaxConcurrent == 0 {
		o.MaxConcurrent = 1
	}
	if o.Timeout <= 0 || o.Timeout > 30*time.Second || o.MaxConcurrent < 1 || o.MaxConcurrent > 4 {
		return nil, ErrHEIFOptions
	}
	resolve := func(path, name string) (string, error) {
		if path != "" && !filepath.IsAbs(path) {
			return "", ErrHEIFOptions
		}
		if path == "" {
			path = name
		}
		if o.Runner != nil {
			return path, nil
		}
		found, err := exec.LookPath(path)
		if err != nil {
			return "", ErrHEIFUnavailable
		}
		absolute, err := filepath.Abs(found)
		if err != nil {
			return "", ErrHEIFUnavailable
		}
		return absolute, nil
	}
	vips, err := resolve(o.VIPSPath, "vips")
	if err != nil {
		return nil, err
	}
	header, err := resolve(o.VIPSHeaderPath, "vipsheader")
	if err != nil {
		return nil, err
	}
	if o.TempDir == "" {
		o.TempDir = os.TempDir()
	}
	temp, err := filepath.Abs(o.TempDir)
	if err != nil {
		return nil, ErrHEIFOptions
	}
	info, err := os.Stat(temp)
	if err != nil || !info.IsDir() || strings.ContainsAny(temp, "[]\x00") {
		return nil, ErrHEIFOptions
	}
	if o.Runner == nil {
		o.Runner = HEIFExecRunner{}
	}
	return &HEIFConverter{vips: vips, header: header, temp: temp, timeout: o.Timeout, runner: o.Runner, slots: make(chan struct{}, o.MaxConcurrent)}, nil
}

// IsHEIF inspects bounded ISO-BMFF file-type boxes, not MIME or filename hints.
// It is classification only: the native metadata probe still validates the file.
// AVIF and ordinary MP4 are deliberately outside this upload extension.
func IsHEIF(raw []byte) bool {
	for offset, boxes := uint64(0), 0; boxes < 16 && offset < 4096; boxes++ {
		if offset+8 > uint64(len(raw)) {
			return false
		}
		size := uint64(binary.BigEndian.Uint32(raw[offset : offset+4]))
		kind := string(raw[offset+4 : offset+8])
		header := uint64(8)
		if size == 1 {
			if offset+16 > uint64(len(raw)) {
				return false
			}
			size, header = binary.BigEndian.Uint64(raw[offset+8:offset+16]), 16
		}
		if size < header || size > uint64(len(raw))-offset {
			return false
		}
		if kind == "ftyp" {
			payload := raw[offset+header : offset+size]
			if len(payload) < 8 || len(payload)%4 != 0 || len(payload) > 4096 {
				return false
			}
			found := false
			for i := 0; i < len(payload); i += 4 {
				if i == 4 { // Minor version is not a compatible brand.
					continue
				}
				switch string(payload[i : i+4]) {
				case "avif", "avis":
					return false
				case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
					found = true
				}
			}
			return found
		}
		if kind != "free" && kind != "skip" {
			return false
		}
		offset += size
	}
	return false
}

// Normalize accepts JPEG/PNG/WebP through the existing normalizer and HEIC/HEIF
// through libvips. The deadline covers the queue, probes and native conversion.
// As with other io.Reader APIs, callers must bound/cancel blocking input reads.
// Only the primary HEIF image is read; no thumbnail, resize, crop or page stack.
func (c *HEIFConverter) Normalize(parent context.Context, source io.Reader) (Image, error) {
	if c == nil || c.runner == nil {
		return Image{}, ErrHEIFUnavailable
	}
	if source == nil {
		return Image{}, ErrInvalidImage
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return Image{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(heifContextReader{ctx, source}, MaxImageBytes+1))
	if ctx.Err() != nil {
		return Image{}, ctx.Err()
	}
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	if int64(len(raw)) > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	if !IsHEIF(raw) {
		result, err := normalize(raw)
		if ctx.Err() != nil {
			return Image{}, ctx.Err()
		}
		return result, err
	}
	dir, err := os.MkdirTemp(c.temp, "picpilot-heif-")
	if err != nil {
		return Image{}, ErrHEIFUnavailable
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "source.heic")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		return Image{}, ErrHEIFUnavailable
	}
	env := heifEnvironment(dir, c.vips, c.header)
	probe := func(field string) (uint64, error) {
		out, err := c.run(ctx, HEIFCommand{Path: c.header, Args: []string{"-f", field, input}, Dir: dir, Env: env}, 128)
		if err != nil {
			return 0, err
		}
		text := strings.TrimSpace(string(out))
		if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return 0, ErrInvalidImage
		}
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil || n > uint64(MaxImagePixels) {
			return 0, ErrImageTooLarge
		}
		if n == 0 {
			return 0, ErrInvalidImage
		}
		return n, nil
	}
	width, err := probe("width")
	if err != nil {
		return Image{}, err
	}
	height, err := probe("height")
	if err != nil {
		return Image{}, err
	}
	if width > uint64(MaxImagePixels)/height {
		return Image{}, ErrImageTooLarge
	}
	orientation := heifOrientation(raw)
	// A suffix-only output selects stdout in libvips. No unbounded output file
	// can fill the temp volume; crossing the byte cap cancels and joins the child.
	// libvips 8.14/libheif does not expose HEIF Exif orientation consistently.
	// Decode without native autorotation, then apply the bounded orientation
	// transform below so the result is deterministic across libvips versions.
	converted, err := c.run(ctx, HEIFCommand{Path: c.vips, Args: []string{"copy", input, ".jpg[Q=92,strip,optimize_coding]", "--vips-concurrency=1", "--vips-cache-max=0"}, Dir: dir, Env: env}, MaxImageBytes)
	if err != nil {
		return Image{}, err
	}
	conf, err := jpeg.DecodeConfig(bytes.NewReader(converted))
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	if conf.Width <= 0 || conf.Height <= 0 || int64(conf.Width) > MaxImagePixels/int64(conf.Height) {
		return Image{}, ErrImageTooLarge
	}
	w, h := uint64(conf.Width), uint64(conf.Height)
	if (w != width || h != height) && (w != height || h != width) {
		return Image{}, ErrInvalidImage
	}
	if ctx.Err() != nil {
		return Image{}, ctx.Err()
	}
	decoded, err := jpeg.Decode(bytes.NewReader(converted))
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	if ctx.Err() != nil {
		return Image{}, ctx.Err()
	}
	decoded = applyHEIFOrientation(decoded, orientation)
	resultWidth, resultHeight := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if resultWidth <= 0 || resultHeight <= 0 || int64(resultWidth) > MaxImagePixels/int64(resultHeight) {
		return Image{}, ErrImageTooLarge
	}
	// Re-encoding with the standard library also strips metadata independently
	// of native strip support. The decoded pixels now have orientation materialized.
	out := heifBuffer{limit: MaxImageBytes, cancel: cancel}
	if err := jpeg.Encode(&out, decoded, &jpeg.Options{Quality: 92}); err != nil {
		return Image{}, ErrImageTooLarge
	}
	if ctx.Err() != nil {
		return Image{}, ctx.Err()
	}
	sum, original := sha256.Sum256(out.Bytes()), sha256.Sum256(raw)
	return Image{Bytes: out.Bytes(), ContentType: "image/jpeg", Extension: "jpg", Width: resultWidth, Height: resultHeight, SHA256: hex.EncodeToString(sum[:]), SourceSHA256: hex.EncodeToString(original[:])}, nil
}

// heifOrientation extracts only the bounded TIFF orientation field from an
// embedded Exif payload. HEIF Exif blocks are not necessarily exposed by the
// libvips metadata probe, so this parser keeps orientation handling stable
// without trusting filenames, MIME types, or unbounded offsets.
func heifOrientation(raw []byte) uint8 {
	marker := []byte{'E', 'x', 'i', 'f', 0, 0}
	for from := 0; from < len(raw); {
		i := bytes.Index(raw[from:], marker)
		if i < 0 {
			return 1
		}
		start := from + i + len(marker)
		if orientation, ok := parseHEIFTIFFOrientation(raw[start:]); ok {
			return orientation
		}
		from = start
	}
	return 1
}

func parseHEIFTIFFOrientation(raw []byte) (uint8, bool) {
	if len(raw) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(raw[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(raw[2:4]) != 42 {
		return 0, false
	}
	ifdOffset := uint64(order.Uint32(raw[4:8]))
	if ifdOffset > uint64(len(raw)-2) {
		return 0, false
	}
	ifd := raw[ifdOffset:]
	count := uint64(order.Uint16(ifd[:2]))
	if count > 512 || count > uint64((len(ifd)-2)/12) {
		return 0, false
	}
	for i := uint64(0); i < count; i++ {
		entry := ifd[2+i*12 : 2+(i+1)*12]
		if order.Uint16(entry[0:2]) != 0x0112 || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			continue
		}
		orientation := order.Uint16(entry[8:10])
		if orientation >= 1 && orientation <= 8 {
			return uint8(orientation), true
		}
		return 0, false
	}
	return 0, false
}

func applyHEIFOrientation(img image.Image, orientation uint8) image.Image {
	switch orientation {
	case 2:
		return imaging.FlipH(img)
	case 3:
		return imaging.Rotate180(img)
	case 4:
		return imaging.FlipV(img)
	case 5:
		return imaging.Transpose(img)
	case 6:
		return imaging.Rotate270(img)
	case 7:
		return imaging.Transverse(img)
	case 8:
		return imaging.Rotate90(img)
	default:
		return img
	}
}

func (c *HEIFConverter) run(parent context.Context, command HEIFCommand, limit int64) ([]byte, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	out, diagnostics := &heifBuffer{limit: limit, cancel: cancel}, &heifBuffer{limit: 4096, cancel: cancel}
	command.Stdout, command.Stderr = out, diagnostics
	err := c.runner.Run(ctx, command)
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	if out.exceeded {
		if limit == MaxImageBytes {
			return nil, ErrImageTooLarge
		}
		return nil, ErrInvalidImage
	}
	if diagnostics.exceeded {
		return nil, ErrInvalidImage
	}
	if errors.Is(err, ErrHEIFUnavailable) {
		return nil, ErrHEIFUnavailable
	}
	if err != nil {
		return nil, ErrInvalidImage
	}
	return out.Bytes(), nil
}

type heifContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r heifContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type heifBuffer struct {
	buf      bytes.Buffer
	limit    int64
	exceeded bool
	cancel   context.CancelFunc
}

func (b *heifBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *heifBuffer) Len() int      { return b.buf.Len() }

func (b *heifBuffer) Write(p []byte) (int, error) {
	left := b.limit - int64(b.Len())
	if int64(len(p)) > left {
		b.exceeded = true
		b.cancel()
		n, _ := b.buf.Write(p[:int(left)])
		return n, ErrImageTooLarge
	}
	return b.buf.Write(p)
}

func heifEnvironment(dir, vips, header string) []string {
	env := []string{"LC_ALL=C", "LANG=C", "VIPS_CONCURRENCY=1", "TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir, "HOME=" + dir,
		"PATH=" + filepath.Dir(vips) + string(os.PathListSeparator) + filepath.Dir(header)}
	// The Windows loader needs SystemRoot. No caller/provider environment, DLL
	// injection variables, or LIBHEIF_SECURITY_LIMITS=off are forwarded.
	if runtime.GOOS == "windows" {
		if root := os.Getenv("SystemRoot"); root != "" {
			env = append(env, "SystemRoot="+root)
		}
	}
	return env
}

// HEIFExecRunner invokes an absolute trusted program directly. Limits and
// cancellation are supplied by HEIFConverter; no shell or retry is involved.
type HEIFExecRunner struct{}

func (HEIFExecRunner) Run(ctx context.Context, c HEIFCommand) error {
	if !filepath.IsAbs(c.Path) {
		return ErrHEIFUnavailable
	}
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = c.Dir, c.Env, c.Stdout, c.Stderr
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrHEIFUnavailable
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return ErrInvalidImage
	}
	return nil
}
