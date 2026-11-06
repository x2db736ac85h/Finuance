// Package images checks and re-encodes the photos users attach to assistant questions (Phase 20,
// receipts by photo). Pure: no database, no HTTP. The browser already re-encodes a photo to a JPEG
// of at most 1,600 px; the server trusts none of that. It reads the dimensions from the header
// before decoding anything (a small file can claim a huge image), then decodes and encodes the
// pixels again, so nothing but pixels survives (no EXIF, no location, no comments, no thumbnails).
package images

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
)

// Limits.
const (
	MaxBytes  = 2 << 20 // 2 MiB, before and after re-encoding
	MaxSide   = 2048    // px; the browser sends at most 1,600
	maxPixels = MaxSide * MaxSide
	quality   = 85
)

// Errors (each wraps ErrInvalid).
var (
	ErrInvalid  = errors.New("invalid image")
	ErrTooLarge = fmt.Errorf("%w: larger than %d bytes", ErrInvalid, MaxBytes)
)

// Image is a checked, re-encoded JPEG.
type Image struct {
	Data          []byte
	Width, Height int
}

// Normalize checks a JPEG and encodes its pixels again.
func Normalize(data []byte) (Image, error) {
	switch {
	case len(data) == 0:
		return Image{}, fmt.Errorf("%w: empty", ErrInvalid)
	case len(data) > MaxBytes:
		return Image{}, ErrTooLarge
	case len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff:
		return Image{}, fmt.Errorf("%w: not a JPEG", ErrInvalid)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %v", ErrInvalid, err) //nolint:errorlint // one wrapped sentinel
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > maxPixels {
		return Image{}, fmt.Errorf("%w: %d × %d px (at most %d px a side)", ErrInvalid, cfg.Width, cfg.Height, MaxSide)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %v", ErrInvalid, err) //nolint:errorlint // one wrapped sentinel
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return Image{}, fmt.Errorf("%w: %v", ErrInvalid, err) //nolint:errorlint // one wrapped sentinel
	}
	if out.Len() > MaxBytes {
		return Image{}, ErrTooLarge
	}
	b := img.Bounds()
	return Image{Data: out.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}
