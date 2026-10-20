package main

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// source remembers how a file was encoded so the output can use exactly the same bytes layout.
type source struct {
	Enc string // "utf-8", "windows-1250", "iso-8859-2", "windows-1252"
	BOM bool
}

var charmaps = map[string]encoding.Encoding{
	"windows-1250": charmap.Windows1250,
	"iso-8859-2":   charmap.ISO8859_2,
	"windows-1252": charmap.Windows1252,
}

// decode follows the legacy auto rule: strict UTF-8 (BOM stripped), else Windows-1250.
func decode(b []byte, enc string) (string, source, error) {
	src := source{Enc: enc}
	if enc == "auto" || enc == "utf-8" {
		if bytes.HasPrefix(b, utf8BOM) {
			src.BOM, b = true, b[len(utf8BOM):]
		}
		if utf8.Valid(b) {
			src.Enc = "utf-8"
			return string(b), src, nil
		}
		if enc == "utf-8" {
			return "", src, fmt.Errorf("not valid UTF-8")
		}
		src.Enc = "windows-1250"
	}
	cm, ok := charmaps[src.Enc]
	if !ok {
		return "", src, fmt.Errorf("unsupported encoding %q", enc)
	}
	out, err := cm.NewDecoder().Bytes(b)
	if err != nil {
		return "", src, fmt.Errorf("decode %s: %w", src.Enc, err)
	}
	return string(out), src, nil
}

func encode(text string, src source) ([]byte, error) {
	var out []byte
	if src.Enc == "utf-8" {
		out = []byte(text)
	} else {
		var err error
		// x/text charmap encoders fail on characters the charset lacks instead of writing '?'.
		out, err = charmaps[src.Enc].NewEncoder().Bytes([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", src.Enc, err)
		}
	}
	if src.BOM {
		out = append(append([]byte{}, utf8BOM...), out...)
	}
	return out, nil
}
