package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func photo(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255}) //nolint:gosec // test pattern
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withExif puts an APP1 Exif segment with a GPS-looking payload right after the SOI marker.
func withExif(j []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("GPS 52.2297N 21.0122E camera Phone")...)
	seg := []byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	return append(append(append([]byte{}, j[:2]...), append(seg, payload...)...), j[2:]...)
}

// claiming patches the frame header (SOF0) to claim another size.
func claiming(t *testing.T, j []byte, w, h int) []byte {
	t.Helper()
	out := append([]byte{}, j...)
	i := bytes.Index(out, []byte{0xff, 0xc0})
	if i < 0 {
		t.Fatal("no SOF0")
	}
	out[i+5], out[i+6], out[i+7], out[i+8] = byte(h>>8), byte(h), byte(w>>8), byte(w)
	return out
}

func TestNormalize(t *testing.T) {
	src := withExif(photo(t, 64, 48))
	if !bytes.Contains(src, []byte("Exif")) {
		t.Fatal("setup")
	}
	got, err := Normalize(src)
	if err != nil || got.Width != 64 || got.Height != 48 {
		t.Fatalf("%v %+v", err, got)
	}
	if bytes.Contains(got.Data, []byte("Exif")) || bytes.Contains(got.Data, []byte("GPS")) {
		t.Error("metadata survived the re-encoding")
	}
	if _, err := jpeg.Decode(bytes.NewReader(got.Data)); err != nil {
		t.Errorf("output is not a JPEG: %v", err)
	}

	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	big := photo(t, 16, 16)
	cases := map[string][]byte{
		"empty":                 nil,
		"a PNG":                 pngBuf.Bytes(),
		"over 2 MiB":            append(append([]byte{}, big...), make([]byte, MaxBytes)...),
		"claims 60,000 px":      claiming(t, big, 60000, 60000),
		"claims 2,049 px wide":  claiming(t, big, 2049, 16),
		"cut off after its SOF": big[:bytes.Index(big, []byte{0xff, 0xda})+4],
	}
	for name, data := range cases {
		if _, err := Normalize(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
