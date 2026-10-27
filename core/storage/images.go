package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const MaxImageBytes int64 = 25 << 20
const MaxImagePixels int64 = 50_000_000

var ErrInvalidImage = errors.New("invalid or unsupported image")
var ErrImageTooLarge = errors.New("image exceeds the byte or pixel limit")

type Image struct {
	Bytes        []byte
	ContentType  string
	Extension    string
	Width        int
	Height       int
	SHA256       string
	SourceSHA256 string
}

// NormalizeImage decodes by file signature, honors EXIF orientation and removes
// metadata. It never crops, stretches, upscales or invents a requested size.
func NormalizeImage(r io.Reader) (Image, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	if int64(len(raw)) > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	return normalize(raw)
}

func normalize(raw []byte) (Image, error) {
	c, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !allowedFormat(format) {
		return Image{}, ErrInvalidImage
	}
	if c.Width <= 0 || c.Height <= 0 || int64(c.Width) > MaxImagePixels/int64(c.Height) {
		return Image{}, ErrImageTooLarge
	}
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	var out bytes.Buffer
	result := Image{Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy(), ContentType: "image/png", Extension: "png"}
	if format == "jpeg" {
		result.ContentType = "image/jpeg"
		result.Extension = "jpg"
		err = jpeg.Encode(&out, decoded, &jpeg.Options{Quality: 95})
	} else {
		encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
		err = encoder.Encode(&out, decoded)
	}
	if err != nil {
		return Image{}, ErrInvalidImage
	}
	if int64(out.Len()) > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	result.Bytes = out.Bytes()
	sum := sha256.Sum256(result.Bytes)
	source := sha256.Sum256(raw)
	result.SHA256 = hex.EncodeToString(sum[:])
	result.SourceSHA256 = hex.EncodeToString(source[:])
	return result, nil
}

func allowedFormat(format string) bool {
	return format == "png" || format == "jpeg" || format == "webp"
}
