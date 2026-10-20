package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Encoding names a text encoding of an import file.
type Encoding string

// Supported encodings. Auto is the default: strict UTF-8, else Windows-1250 (the most common
// encoding of Polish bank exports).
const (
	EncodingAuto        Encoding = "auto"
	EncodingUTF8        Encoding = "utf-8"
	EncodingWindows1250 Encoding = "windows-1250"
	EncodingISO88592    Encoding = "iso-8859-2"
	EncodingWindows1252 Encoding = "windows-1252"
)

// ErrUnknownEncoding is returned for an encoding name not listed above.
var ErrUnknownEncoding = errors.New("unknown encoding")

// Label is the human-readable name the legacy app showed (and the oracle recorded).
func (e Encoding) Label() string {
	switch e {
	case EncodingUTF8:
		return "UTF-8"
	case EncodingWindows1250:
		return "Windows-1250"
	case EncodingISO88592:
		return "ISO-8859-2"
	case EncodingWindows1252:
		return "Windows-1252"
	}
	return string(e)
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Decode turns file bytes into text and reports the encoding actually used (legacy `decode`).
//
//   - auto: a leading UTF-8 BOM is dropped; if the rest is valid UTF-8 it is UTF-8, otherwise the
//     bytes are read as Windows-1250. A Windows-1250 file almost never happens to be valid UTF-8,
//     because its Polish letters are single bytes ≥ 0x80 that UTF-8 only allows in sequences.
//   - an explicit encoding is applied as is. Explicit UTF-8 replaces invalid sequences with U+FFFD
//     instead of failing, like the browser's non-fatal TextDecoder.
func Decode(b []byte, enc Encoding) (string, Encoding, error) {
	switch enc {
	case EncodingAuto, "":
		rest := bytes.TrimPrefix(b, utf8BOM)
		if utf8.Valid(rest) {
			return string(rest), EncodingUTF8, nil
		}
		return decodeSingleByte(b, charmap.Windows1250), EncodingWindows1250, nil
	case EncodingUTF8:
		return decodeUTF8Lossy(bytes.TrimPrefix(b, utf8BOM)), EncodingUTF8, nil
	case EncodingWindows1250:
		return decodeSingleByte(b, charmap.Windows1250), enc, nil
	case EncodingISO88592:
		return decodeSingleByte(b, charmap.ISO8859_2), enc, nil
	case EncodingWindows1252:
		return decodeSingleByte(b, charmap.Windows1252), enc, nil
	}
	return "", enc, fmt.Errorf("%w: %q", ErrUnknownEncoding, enc)
}

// decodeSingleByte maps every byte through the code page. The WHATWG encodings the browser uses
// map the few unassigned bytes of Windows-1250/1252 (0x81, 0x83, 0x88, 0x90, 0x98 …) to the C1
// control character with the same value; x/text yields U+FFFD for them. We follow the browser.
func decodeSingleByte(b []byte, cm *charmap.Charmap) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
			continue
		}
		r := cm.DecodeByte(c)
		if r == utf8.RuneError {
			r = rune(c)
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// decodeUTF8Lossy is the WHATWG UTF-8 decoder in replacement mode: each "maximal subpart" of an
// ill-formed sequence becomes one U+FFFD. Go's range loop instead emits one U+FFFD per bad byte,
// which differs for truncated multi-byte sequences ("E6 9C 41" → "�A" here, two U+FFFD in Go).
func decodeUTF8Lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || n > 1 {
			sb.WriteRune(r)
			i += n
			continue
		}
		sb.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return sb.String()
}

// maximalSubpart returns how many bytes of an ill-formed sequence starting at b[0] form one
// maximal subpart (Unicode §3.9, table 3-7): the lead byte plus every continuation byte that was
// still valid for it.
func maximalSubpart(b []byte) int {
	lead := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, lo = 2, 0xA0
	case lead == 0xED:
		need, hi = 2, 0x9F
	case lead >= 0xE1 && lead <= 0xEF:
		need = 2
	case lead == 0xF0:
		need, lo = 3, 0x90
	case lead == 0xF4:
		need, hi = 3, 0x8F
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	default:
		return 1
	}
	n := 1
	for k := 0; k < need && n < len(b); k++ {
		c := b[n]
		if c < lo || c > hi {
			break
		}
		lo, hi = 0x80, 0xBF
		n++
	}
	return n
}
