// Package ingest turns the raw bytes of a bank CSV export into typed, canonical rows. It is a pure
// package: no database, no HTTP, no clock. Every rule is a port of the legacy single-file app
// (legacy/finuance.html) and is tested against the legacy oracle in testdata/oracle/.
//
// The pipeline, in order (see Parse):
//
//	Decode            bytes → text (auto: UTF-8, else Windows-1250)            decode.go
//	DetectDelimiter   ; , tab | — most consistent count outside quotes        delimiter.go
//	Tokenize          RFC-4180-ish state machine → records of fields          csv.go
//	Shape             header row, footer drop, trim, ' strip, names           table.go
//	AnalyzeColumn     per column: date / number / text                        columns.go
//	DetectRoles       which column is the date, amount, payee …               columns.go
//	Stage             mapped canonical rows with per-row errors               stage.go
//	Fingerprint       the de-duplication identity of a row                    fingerprint.go
//
// Where the legacy app relied on JavaScript semantics that differ from Go's (Unicode whitespace in
// trim() and \s, the WHATWG text decoders), small helpers in js.go reproduce the JavaScript
// behaviour exactly. Intentional differences are listed in testdata/oracle/ALLOWED_DIFFS.md.
package ingest
