# Anonymised real exports

Real bank exports run through `backend/cmd/anonymize` and reviewed by Irwin before commit (2026-10-20).
Structure (encoding, BOM, delimiter, quoting, line endings, preamble, padding) is the bank's; every
name, number, place and amount is replaced. Category, currency and operation-type columns are kept.

| Bank | File | Encoding | Notes |
|---|---|---|---|
| Pekao | `pekao/pekao_example.csv` | UTF-8, LF | Works in the MVP |
| PKO BP | `pko-bp/pko_bp_example.csv` | Windows-1250, CRLF | MVP picks a data row as the header |
| mBank | `mbank/mbank_example.csv` | UTF-8 BOM, CRLF | MVP picks the bank address line as the header |
