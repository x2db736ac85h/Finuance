# anonymize

Turns real bank CSV exports into fixtures that can be committed. The structure stays the same and the personal data goes.

```sh
export FINUANCE_ANON_KEY="$(head -c 32 /dev/urandom | base64)"   # keep it with the private files, never commit it
cd backend
go run ./cmd/anonymize -out ../testdata/fixtures/real/pekao ../testdata/private/pekao_*.csv
```

| Flag | Default | Meaning |
|---|---|---|
| `-key` | `$FINUANCE_ANON_KEY` | HMAC-SHA256 key. The same key gives the same output, so files of one account stay consistent across runs. |
| `-out` | required | Output directory. Inputs are never overwritten. Digit runs of 6+ in file names are replaced. |
| `-shift-days` | derived from the key (30–400 days back) | Constant shift applied to every date. |
| `-enc` | `auto` | `auto` (UTF-8, else Windows-1250, like the legacy app), `utf-8`, `windows-1250`, `iso-8859-2`, `windows-1252`. |
| `-delim` | `auto` | `auto`, `;`, `,`, `tab`, `pipe`. |

The tool prints only structural facts (encoding, delimiter, record count, header record, column roles), never cell contents. **Always review the output before committing it.**

## What is preserved

The output keeps the following, byte for byte:
- the encoding and BOM;
- the delimiter;
- the quoting of every field, including `""` escapes and quoted newlines;
- every line ending (LF/CRLF/CR, mixed), blank lines and the final newline;
- the preamble line count and the field count of every record.

## Rules

**Header.** The tool takes the closest non-blank record before the first data record, provided it holds no dates, numbers or long digit runs. "Data record" means a record with the modal field count among records that hold a date or amount. The header row is copied verbatim, apart from digit runs of 6+.

This rule handles the files where the legacy rule fails: mBank's long preamble (14 two-field lines outnumber the data rows), and PKO BP's 6 named + 5 blank header columns.

**Column roles** come from the header names:

| Role | Header names (normalised) | Treatment |
|---|---|---|
| keep | `waluta`, `currency`, `kategor…`, `typ…`, `rodzaj…`, `type` | Copied verbatim (bank vocabulary, not personal). |
| amount | `kwota`, `amount`, `saldo`, `balance`, `wpływy`, `wydatki`, `obciążenia`, `uznania`, `debit`, `credit`, … | Every number is scaled. |
| payee | `nadawca`, `odbiorca`, `kontrahent`, `payee`, `merchant`, … (not `adres…`) | Fake merchant + fake city. |
| free | everything else, plus the preamble | Word, digit, date and amount rules below. |

**Values:**
- **Dates** (`yyyy-mm-dd`, `dd.mm.yyyy`, `d.m.yyyy`, …, with optional time) are shifted by `-shift-days`, keeping separators and zero-padding. Impossible dates (`31.02.2025`) are left as text.
- **Amounts** are multiplied by a key-derived factor in [0.7, 1.3] and rounded to the original number of decimals. The sign, currency prefix/suffix (`-800,00 PLN`, `-€12,50`), thousands separator and decimal separator are all kept. The same original amount always maps to the same output, so de-duplication behaviour is unchanged.
  - In free text, only standalone two-decimal numbers count (`Oryginalna kwota operacji: 43.52`). `28.02-28.03` is a date range and is left alone.
- **Digit runs of 6+**, and any digit run next to a `*` card mask, become fake digits of the same length. 26-digit account numbers become the all-zero form `0000…0nnnnn` that `tools/check-private.sh` allowlists. Spaced IBANs (`PL61 1090 …`) keep their spacing.
- **Codes** of 8+ characters that mix letters and digits (references, terminal ids) are replaced character by character.
- **Payees** `NAME    CITY`: the name becomes a fake merchant and the city an invented place name. The padding is adjusted so the city starts in the same column.
- **Words** longer than 2 letters that aren't in the banking allowlist (`words.go`) become pronounceable fake words of the same length and case pattern. The same word maps to the same fake word everywhere.

## Limitations

- Personal data inside **kept** columns (an unusual custom category name) stays. Review the output.
- Words of 1–2 letters (initials) are kept.
- Free-text integers shorter than 6 digits (house numbers, times) are kept.

## Tests

`go test ./cmd/anonymize/` runs on synthetic data only. The tests cover:
- consistent mapping across files and determinism per key;
- no sensitive token surviving in Pekao-, PKO BP- and mBank-shaped inputs;
- byte-identical parse/write round trips for every synthetic fixture;
- an unchanged record layout;
- kept columns and header;
- the date, amount, card-mask and padding rules;
- header detection;
- the CLI.
