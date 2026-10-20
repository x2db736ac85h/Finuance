// Command anonymize turns real bank CSV exports into fixtures that keep the file structure
// (encoding, delimiter, quoting, line endings, preamble, column count, padding) but contain no
// personal data. See README.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "anonymize:", err)
		os.Exit(1)
	}
}

func run(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("anonymize", flag.ContinueOnError)
	fs.SetOutput(log)
	key := fs.String("key", "", "HMAC key (default: $FINUANCE_ANON_KEY)")
	shift := fs.Int("shift-days", 0, "shift every date by this many days (0 = derive 30–400 days back from the key)")
	out := fs.String("out", "", "output directory (required)")
	enc := fs.String("enc", "auto", "input encoding: auto, utf-8, windows-1250, iso-8859-2, windows-1252")
	delim := fs.String("delim", "auto", "delimiter: auto, ';', ',', tab, pipe")
	fs.Usage = func() {
		fmt.Fprintln(log, "usage: anonymize -out DIR [-key K] [-shift-days N] [-enc E] [-delim D] FILE...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		*key = os.Getenv("FINUANCE_ANON_KEY")
	}
	switch {
	case *key == "":
		return errors.New("a key is required (-key or FINUANCE_ANON_KEY)")
	case *out == "":
		return errors.New("-out is required")
	case fs.NArg() == 0:
		return errors.New("no input files")
	}
	d, err := parseDelim(*delim)
	if err != nil {
		return err
	}
	a := newAnonymizer(*key, *shift)
	fmt.Fprintf(log, "shift-days=%d amount-factor=%.3f\n", a.shiftDays, a.factor)
	if err := os.MkdirAll(*out, 0o755); err != nil { //nolint:gosec // fixtures are committed and must be readable
		return err
	}
	for _, in := range fs.Args() {
		dst := filepath.Join(*out, a.digitsOnly(filepath.Base(in)))
		if same(in, dst) {
			return fmt.Errorf("%s: refusing to overwrite the input", in)
		}
		b, err := os.ReadFile(in) //nolint:gosec // reading the user-named input file is the tool's job
		if err != nil {
			return err
		}
		res, rep, err := a.file(b, *enc, d)
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
		if err := os.WriteFile(dst, res, 0o644); err != nil { //nolint:gosec // fixtures are committed and must be readable
			return err
		}
		// Only structural facts are printed, never cell contents.
		fmt.Fprintf(log, "%s -> %s: %s\n", filepath.Base(in), dst, rep)
	}
	return nil
}

func parseDelim(s string) (rune, error) {
	switch strings.ToLower(s) {
	case "auto":
		return 0, nil
	case ";", ",", "|":
		return rune(s[0]), nil
	case "tab", `\t`:
		return '\t', nil
	case "pipe":
		return '|', nil
	}
	return 0, fmt.Errorf("unknown delimiter %q", s)
}

func same(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && x == y
}
