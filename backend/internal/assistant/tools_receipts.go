package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Receipts by photo (Phase 20, ADR 0032). The model reads the photo; find_receipt_match looks for
// the transaction it belongs to (read-only), and attach_receipt proposes to write the receipt into
// that transaction's note. attach_receipt repeats the receipt's amount, date and shop instead of a
// transaction id (small models garble ids) and resolves the same match again.

// ToolReceipt is the receipt lookup.
const ToolReceipt = "find_receipt_match"

// ActionAttachReceipt writes a receipt into a matched transaction's note.
const ActionAttachReceipt = "attach_receipt"

const (
	receiptDays       = 3 // a card payment books up to 3 days after the receipt
	receiptCandidates = 5
	receiptPrefix     = "Receipt: "
	maxReceiptItems   = 200
	maxNoteLen        = 2000 // ledger maxNote
)

// receiptStopWords are words of shop names that say nothing about which shop it is.
var receiptStopWords = map[string]bool{"the": true, "and": true, "und": true, "les": true, "des": true, "del": true, "della": true,
	"shop": true, "store": true, "sklep": true, "market": true, "markt": true, "gmbh": true, "ltd": true, "sarl": true, "srl": true,
	"spolka": true, "z.o.o": true, "zoo": true}

// shopWords are the words of a shop's name that can identify it in a payee.
func shopWords(shop string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(ingest.Norm(shop), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if utf8.RuneCountInString(w) >= 3 && !receiptStopWords[w] && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// receiptMatch is a candidate with what ranks it.
type receiptMatch struct {
	row   query.ReceiptRow
	gap   int // days between the receipt and the booking
	words int // words of the shop's name found in the payee or the title
}

// matchReceipt ranks candidates in the receipt's currency (any, when unknown): the nearest day
// first, then the most words of the shop's name in the payee or title.
func matchReceipt(rows []query.ReceiptRow, on platform.Date, shop, currency string) []receiptMatch {
	words := shopWords(shop)
	var out []receiptMatch
	for i := range rows {
		r := &rows[i]
		if currency != "" && r.Currency != currency {
			continue
		}
		m := receiptMatch{row: *r, gap: abs(on.DaysUntil(r.BookedOn))}
		text := ingest.Norm(r.Payee + " " + r.Description)
		for _, w := range words {
			if strings.Contains(text, w) {
				m.words++
			}
		}
		out = append(out, m)
	}
	slices.SortStableFunc(out, func(a, b receiptMatch) int {
		if a.gap != b.gap {
			return a.gap - b.gap
		}
		return b.words - a.words
	})
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// receiptArgs reads the receipt's amount, date, shop and currency; msg is for the model.
func (t *Tools) receiptArgs(ctx context.Context, userID uuid.UUID, a args) (amount int64, on platform.Date, shop, currency, msg string, err error) {
	currency = strings.ToUpper(strings.TrimSpace(a.str("currency")))
	cur := currency
	if cur == "" {
		if cur, err = t.mainCurrency(ctx, userID); err != nil {
			return
		}
	}
	v, ok := positiveMinor(a.str("amount"), cur)
	if !ok {
		msg = "amount must be the amount paid on the receipt, a positive number"
		return
	}
	d := query.ParseToolDate(a.str("date"))
	if d == nil {
		msg = "date must be the receipt's date, YYYY-MM-DD"
		return
	}
	for _, k := range []string{"shop", "payee", "merchant"} { // what models call it
		if shop = strings.TrimSpace(a.str(k)); shop != "" {
			break
		}
	}
	return v, *d, shop, currency, "", nil
}

type receiptCandidateOut struct {
	Date       string      `json:"date"`
	Account    string      `json:"account"`
	Payee      string      `json:"payee"`
	Amount     json.Number `json:"amount"`
	Category   string      `json:"category,omitempty"`
	DaysApart  int         `json:"days_apart"`
	ShopWords  int         `json:"shop_words_in_payee"`
	HasReceipt bool        `json:"has_receipt,omitempty"`
	Typed      bool        `json:"added_by_hand,omitempty"`
}

// receipt is find_receipt_match.
func (t *Tools) receipt(ctx context.Context, userID uuid.UUID, sc Scope, a args) (Outcome, error) {
	amount, on, shop, currency, msg, err := t.receiptArgs(ctx, userID, a)
	if err != nil || msg != "" {
		return outcome(map[string]string{"error": msg}, "receipt match · invalid arguments"), err
	}
	rows, err := t.eng.ReceiptCandidates(ctx, userID, amount, on, receiptDays)
	if err != nil {
		return Outcome{}, err
	}
	ms := matchReceipt(rows, on, shop, currency)
	out := struct {
		Candidates []receiptCandidateOut `json:"candidates"`
		Note       string                `json:"note"`
	}{Candidates: []receiptCandidateOut{}}
	for i := range ms {
		if len(out.Candidates) == receiptCandidates {
			break
		}
		m := &ms[i]
		r := &m.row
		out.Candidates = append(out.Candidates, receiptCandidateOut{Date: r.BookedOn.String(), Account: sc.mask(r.AccountName),
			Payee: sc.mask(ingest.Collapse(r.Payee)), Amount: amount2(r.AmountMinor, r.Currency), Category: r.Category, DaysApart: m.gap,
			ShopWords: m.words, HasReceipt: strings.Contains(r.Note, receiptPrefix), Typed: r.Typed})
	}
	if len(out.Candidates) == 0 {
		out.Note = fmt.Sprintf("No money out of exactly this amount within %d days of the date. If it was paid in cash or the bank's file "+
			"isn't imported yet, propose add_expense (a cash account, or the bank account the card belongs to).", receiptDays)
	} else {
		out.Note = "If one candidate is the receipt's payment, propose attach_receipt with the same amount, date and shop (and account " +
			"when several fit). A candidate with has_receipt has one in its note already."
	}
	return outcome(out, fmt.Sprintf("receipt match · %d %s", len(out.Candidates), plural(len(out.Candidates), "candidate", "candidates"))), nil
}

// amount2 is amount() for a money-out row: the positive amount paid.
func amount2(minor int64, currency string) json.Number { return amount(-minor, currency) }

// proposeReceipt is attach_receipt.
func (t *Tools) proposeReceipt(ctx context.Context, userID uuid.UUID, sc Scope, a args, p *Proposal) (Outcome, error) {
	amount, on, shop, currency, msg, err := t.receiptArgs(ctx, userID, a)
	if err != nil || msg != "" {
		return proposalError(msg), err
	}
	if shop == "" {
		return proposalError("shop must be the shop's name as printed on the receipt"), nil
	}
	rows, err := t.eng.ReceiptCandidates(ctx, userID, amount, on, receiptDays)
	if err != nil {
		return Outcome{}, err
	}
	ms := matchReceipt(rows, on, shop, currency)
	if acc := strings.TrimSpace(a.str("account")); acc != "" {
		ms = slices.DeleteFunc(ms, func(m receiptMatch) bool { return !sameName(m.row.AccountName, acc) })
	}
	switch {
	case len(ms) == 0:
		return proposalError(fmt.Sprintf("No money out of exactly this amount within %d days of %s; propose add_expense instead", receiptDays, on)), nil
	case len(ms) > 1 && ms[0].gap == ms[1].gap && ms[0].words == ms[1].words:
		return proposalError(fmt.Sprintf("%d transactions fit equally well; name the account (account) to pick one", len(ms))), nil
	}
	r := ms[0].row
	line := receiptPrefix + ingest.Collapse(shop)
	if items, ok := cleanText(a.str("items"), maxReceiptItems); ok {
		line += " · " + items
	}
	if strings.Contains(r.Note, line) {
		return proposalError("That transaction has this receipt in its note already"), nil
	}
	note := line
	if strings.TrimSpace(r.Note) != "" {
		note = strings.TrimSpace(r.Note) + "\n" + line
	}
	if utf8.RuneCountInString(note) > maxNoteLen {
		return proposalError("The note would be too long; give a shorter items summary"), nil
	}
	p.IDs, p.Note, p.ReceiptLine = []uuid.UUID{r.ID}, note, line
	p.AccountName, p.Date, p.AmountMinor, p.Currency, p.Payee = r.AccountName, r.BookedOn.String(), r.AmountMinor, r.Currency, ingest.Collapse(r.Payee)
	if name, ok := cleanText(a.str("to_category"), maxProposalName); ok {
		p.CategoryName = name
		c, err := t.findCategory(ctx, userID, name)
		if err != nil {
			return Outcome{}, err
		}
		if c != nil {
			p.CategoryName, p.CategoryID = c.Name, &c.ID
		}
	}
	p.Sample = []ProposalRow{{ID: r.ID, Date: r.BookedOn.String(), Payee: ingest.Collapse(r.Payee), AmountMinor: r.AmountMinor,
		Currency: r.Currency, Category: r.Category}}
	p.Matched = 1
	return proposalOutcome(sc, p, shop), nil
}
