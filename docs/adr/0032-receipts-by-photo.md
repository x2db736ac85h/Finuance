# 0032. Receipts by photo: read by the model, matched read-only, applied as proposals

- Status: accepted
- Date: 2026-11-06

## Context

Phase 20 lets a user photograph a receipt and hand it to the assistant: "here is a receipt". The
assistant should find the bank transaction the receipt belongs to and note what was bought, or add
the expense when there is none (cash, or a card payment the bank hasn't exported yet). The rules of
Phase 20 hold: the model reads, tools only read, every change is a proposal applied by the panel and
recorded as the assistant's (ADR 0030). One universal model on a 4 GB GPU has to read the photo
(PLAN 20.8 measures which).

## Decision

- **Photos** (task 20.6): the browser re-encodes a photo to a JPEG of at most 1,600 px (EXIF and
  location never leave the device); `POST /assistant/chats/{id}/attachments` checks the size and
  the header's dimensions before decoding and re-encodes the pixels again (`internal/images`). A
  photo goes to the model with its question only, needs a model that reads images
  (`Caps.Vision`: Ollama's `/api/show`, LM Studio's list, the model id, or the connection's
  `caps`), and is deleted with its chat. The cloud consent covers photos.
- **Matching is a read-only tool**, `find_receipt_match` (amount paid, date, shop): money out of
  exactly that amount in any of the user's accounts, booked within 3 days of the receipt's date
  (card payments book up to 3 days later), ranked by the date gap and by how many words of the shop's
  name appear in the payee; at most 5 candidates, each with whether its note already holds a receipt.
- **Two outcomes, both proposals**: `attach_receipt` writes "Receipt: shop · what was bought" into
  the matched transaction's note (kept after any note already there) and optionally its category,
  through `PATCH /transactions/{id}`; recorded with the note before and after, so Undo puts the old
  note back. Without a match, `add_expense` as before.
- **Typed rows in bank accounts** (ADR 0029 update): `add_expense` may name a bank account for a
  card payment the bank hasn't exported yet. When the import brings the bank's own row, the two
  look like duplicates; `GET /transactions/merge-suggestions` lists each typed row of a bank
  account with an imported row of the same account, amount and a date within 3 days, and
  `POST /transactions/{id}/merge {into}` keeps the imported row and gives it the typed row's
  category set by hand, note and group, moves its shared-group copy (the shared expense keeps its id
  and split), then deletes the typed row; *Keep both* dismisses the suggestion for good.

## Consequences (positive, negative, follow-ups)

- The receipt never has to be typed, and nothing changes without the user's Apply (or the kinds
  they let the panel apply by itself).
- Matching depends on the model reading the total right; the receipt evals measure exactly that
  (the decoys are subtotals, VAT lines, the cash handed over and the change).
- A photo costs about a thousand tokens of an 8k context; one photo per question is the sensible
  use, three the limit.
- Typed rows in bank accounts can be forgotten when the bank's row is never imported; they stay
  ordinary transactions the user can delete.

## Alternatives considered

- **OCR on the server** (Tesseract or similar): a new dependency and a second model of the
  receipt's layout; the vision model reads receipts in many languages without either.
- **Matching inside the model** (give it the month's transactions): more tokens, and a 4B model
  ranks candidates worse than a query does.
- **Attach the photo itself to the transaction**: keeps more personal data for longer; the note
  carries what matters, and the photo stays with its chat (and its retention).
