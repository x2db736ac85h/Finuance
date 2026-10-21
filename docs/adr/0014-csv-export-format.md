# 0014. CSV export: Polish-Excel-friendly text, streamed from one query

- Status: accepted
- Date: 2026-10-21

## Context

`POST /transactions/export` and the `transactions.csv` inside `GET /me/export` write the user's rows as CSV (PLAN tasks 7.9, 7.10). The files are opened mostly in Excel or LibreOffice with a Polish locale, and are imported back into Finuance or other tools. The legacy export wrote the visible columns in order, with the raw file text, a UTF-8 BOM, the source file's delimiter (`;` by default) and CRLF line breaks, and it had no protection against formula injection. Exports can hold every row of a user, up to about 200k.

## Decision

- **Encoding and layout:** UTF-8 with a BOM, the chosen delimiter (`;` by default; `,`, tab or `|` on request), CRLF between lines, no trailing line break (as the legacy). The header row uses the grid's English column labels; the columns come in the order the client sends them.
- **Values:** the same fixed text form as the value checklist and the text filters (Irwin's Phase 7 decision):
  - amounts as `-1234,56`: a comma decimal mark, no thousands separator, the currency's number of decimals (Excel with a Polish locale reads them as numbers);
  - dates as `dd.mm.yyyy`.
- **Quoting:** a cell is quoted when it contains the delimiter, a double quote or a line break; quotes are doubled.
- **Formula injection guard** (PLAN §8.1): a cell that starts with `=`, `+`, `-`, `@`, tab or CR and does not read as a number gets a leading `'`. Amounts such as `-79,99` stay numbers.
- **Streaming:** one `SELECT` over the compiled context in a read-only `WithUser` transaction. pgx reads rows from the server as they arrive, and the handler writes each line through a buffered pipe to the response, so memory stays flat. Everything that can fail with a problem response (context, sort, columns) is validated before the first byte is written. An error after that cuts the download short and is logged.
- **File name:** `finuance-<view>-<yyyy-mm-dd>.csv`, the view reduced to `[A-Za-z0-9_-]`.

## Consequences

- The export round-trips through Finuance's own importer (comma decimals and `dd.mm.yyyy` are both recognised) and opens correctly in Polish Excel.
- Users with an English-locale spreadsheet see comma-decimal amounts as text; they can choose the `,` delimiter, but the decimal mark stays a comma. A per-user number format is a possible later preference.
- The read transaction stays open for the length of the download. At personal scale (seconds) that is harmless; very slow clients hold a connection from the pool meanwhile.
- A `'`-prefixed cell is no longer byte-identical to the stored text. That is the intended trade-off.

## Alternatives considered

- **The raw text from the bank file:** what the legacy did, but the rebuild stores typed values, and rows merged from different banks would mix formats.
- **Dot decimals and ISO dates:** unambiguous for programs, but Polish Excel turns `-79.99` into text or a date.
- **`DECLARE CURSOR` + `FETCH` in batches:** the same memory profile as a streamed query with more round trips; pgx already streams.
- **Building the file in memory first:** simple and allows a `Content-Length`, but up to tens of MB per request.
