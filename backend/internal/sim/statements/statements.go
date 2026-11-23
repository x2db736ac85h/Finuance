// Package statements writes the traffic simulator's bank statements on demand (PLAN 25.2,
// ADR 0043).
//
// A persona's bank history is a pure function of the persona and the calendar day. Each day's rows
// come from a random stream seeded with the persona's slug and the date, never from a stream that
// starts at the beginning of the requested range, as internal/demo's does (so its rows depend on the
// range). Any range therefore reproduces the same rows. Two overlapping statements de-duplicate
// exactly as real overlapping exports do.
//
// Each persona has two accounts:
//   - a current account: salary, rent, subscriptions, a bill and daily spending;
//   - a savings account, with transfers between the two, so that recurring payments and transfer
//     detection have something to find.
//
// The two accounts are written in the two layouts, so that layout detection and saved import
// profiles are exercised. Every merchant, employer and landlord is invented. Account numbers use
// the all-zero form that tools/check-private.sh allows. Amounts are int64 minor units (grosze).
package statements

import (
	"fmt"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// A persona's two accounts.
const (
	Current = 1
	Savings = 2
)

// Layout is a statement's file layout.
type Layout string

// The two layouts, look-alikes of testdata/fixtures/synthetic/banks/.
const (
	PekaoLike Layout = "pekao-like" // a plain table: dd.mm.yyyy dates, a reference column, UTF-8, LF
	MBankLike Layout = "mbank-like" // a preamble, ISO dates, no reference, a BOM and CRLF
)

// LayoutOf is the layout of a persona's account: personas with an even index write their current
// account Pekao-like and their savings account mBank-like, the others the other way round.
func LayoutOf(p identity.Persona, account int) Layout {
	if (account == Current) == (p.Index%2 == 0) {
		return PekaoLike
	}
	return MBankLike
}

// Number is the account's synthetic number: 26 digits, 20 of them leading zeros.
func Number(p identity.Persona, account int) string {
	return fmt.Sprintf("%020d%04d%02d", 0, 7000+p.Index, account)
}

// AccountName is how the simulator names the account in the instance.
func AccountName(account int) string {
	if account == Savings {
		return "Savings"
	}
	return "Current account"
}

// kind is a row's operation type; each layout writes it in its own words.
type kind int

const (
	card kind = iota
	blik
	incoming    // a transfer from someone else
	outgoing    // a transfer to someone else
	ownOut      // to the persona's other account
	ownIn       // from the persona's other account
	interest    // the savings account's interest
	directDebit // a bill collected by the payee
)

// Row is one line of a statement.
type Row struct {
	Account      int // Current or Savings
	Booked       platform.Date
	Value        platform.Date // the value date (card payments are sometimes a day earlier)
	Payee        string
	Address      string
	Title        string
	Amount       int64 // grosze; negative for money going out
	Kind         kind
	Category     string // the bank's category
	Ref          string // the bank's reference (the Pekao-like layout writes it)
	Counterparty string // the other account's number, for transfers between the persona's accounts
}

// subscription is a fixed monthly payment on a fixed day.
type subscription struct {
	day                    int
	payee, title, category string
	amount                 int64
	kind                   kind
}

var subscriptions = []subscription{
	{5, "STREAMING FILMOWY PLUS", "ABONAMENT MIESIĘCZNY", "Multimedia", -4300, card},
	{8, "MUZYKA BEZ LIMITU", "SUBSKRYPCJA", "Multimedia", -2399, card},
	{12, "TELEFONIA KOMÓRKOWA ALFA", "FAKTURA ZA USŁUGI", "Rachunki", -5999, directDebit},
	{18, "ŚWIATŁOWÓD BETA", "ABONAMENT INTERNETOWY", "Rachunki", -7999, directDebit},
	{22, "KLUB SPORTOWY RUCH", "KARNET MIESIĘCZNY", "Sport", -14900, card},
	{25, "CHMURA NA ZDJĘCIA", "PLAN 200 GB", "Multimedia", -1199, card},
}

// spending is a category of daily purchases: each day each one happens with probability p‰.
type spending struct {
	category string
	payees   []string
	p        int
	lo, hi   int64 // grosze, as a positive amount
	kind     kind
}

var everyday = []spending{
	{"Artykuły spożywcze", []string{"MARKET POD DĘBEM     WARSZAWA", "DELIKATESY ZIELONA   WARSZAWA", "PIEKARNIA KŁOS       WARSZAWA", "WARZYWNIAK U ANI     WARSZAWA"}, 800, 1500, 18000, card},
	{"Restauracje i kawiarnie", []string{"KAWIARNIA ROGALIK    WARSZAWA", "BAR MLECZNY PRZYSTAŃ WARSZAWA", "PIZZERIA KAMIENICA   WARSZAWA"}, 300, 1600, 16000, card},
	{"Transport", []string{"KOMUNIKACJA MIEJSKA  WARSZAWA", "STACJA PALIW PRZY DRODZE", "TAKSÓWKI MIEJSKIE"}, 250, 380, 24000, blik},
	{"Zakupy przez internet", []string{"SKLEP INTERNETOWY GAMMA", "MARKETPLACE DELTA"}, 120, 2500, 48000, blik},
	{"Zdrowie i uroda", []string{"APTEKA POD KOGUTEM   WARSZAWA", "SALON URODY LILIA    WARSZAWA"}, 70, 1500, 22000, card},
	{"Rozrywka", []string{"KINO ŚWIT            WARSZAWA", "TEATR NA WODZIE      WARSZAWA"}, 50, 2400, 15000, card},
	{"Odzież i obuwie", []string{"BUTIK MODNA NITKA    WARSZAWA", "OBUWIE KROK          WARSZAWA"}, 40, 6000, 45000, card},
	{"Dom i ogród", []string{"MARKET BUDOWLANY CEGŁA", "KWIACIARNIA STOKROTKA"}, 40, 2000, 35000, card},
}

var (
	employers = []string{"PRACOWNIA ZIELONY LIŚĆ SP. Z O.O.", "BIURO RACHUNKOWE ABAKUS", "FABRYKA ZABAWEK KRASNAL S.A.",
		"SZKOŁA JĘZYKÓW POLIGLOTA", "STUDIO GRAFICZNE PIKSEL"}
	landlords = []string{"WSPÓLNOTA MIESZKANIOWA POD LIPAMI", "SPÓŁDZIELNIA MIESZKANIOWA ZORZA", "WYNAJEM MIESZKAŃ KASIA"}
)

// profile is a persona's fixed facts, derived from its index (not drawn per day).
type profile struct {
	salary   int64 // grosze, before the day's small variation
	payday   int   // 10 … 14
	rent     int64
	rentDay  int // 1 … 3
	saving   int64
	employer string
	landlord string
	subs     []subscription
	spend    int // the daily probabilities scaled to this percentage (70 … 130)
}

func profileOf(p identity.Persona) profile {
	i := p.Index
	var subs []subscription
	for k, s := range subscriptions {
		if (i+k)%3 != 0 { // four of the six, a different four per persona
			subs = append(subs, s)
		}
	}
	return profile{
		salary:   520_000 + int64((i*37)%11)*45_000, // 5 200 … 9 700 zł
		payday:   10 + i%5,
		rent:     180_000 + int64(i%7)*15_000,
		rentDay:  1 + i%3,
		saving:   30_000 + int64(i%6)*20_000,
		employer: employers[i%len(employers)],
		landlord: landlords[i%len(landlords)],
		subs:     subs,
		spend:    70 + (i*13)%61,
	}
}

// Day returns the persona's rows for one day on both accounts, in the order they were drawn.
func Day(p identity.Persona, d platform.Date) []Row { return DayWith(p, d, History{}) }

// DayWith is Day under a history profile: the default's days are Day's; another profile's are drawn
// from a stream of their own, its spending drawn Density times, its amounts spread and its shops
// multiplied.
func DayWith(p identity.Persona, d platform.Date, h History) []Row {
	h = h.Normal()
	pr := profileOf(p)
	parts := []string{"finuance-sim/v1 day", p.Slug, d.String()}
	if k := h.key(); k != "" {
		parts = append(parts, k)
	}
	s := newStream(parts...)
	cur, sav := Number(p, Current), Number(p, Savings)
	var rows []Row
	add := func(r Row) {
		r.Booked = d
		if r.Value.IsZero() {
			r.Value = d
		}
		r.Ref = fmt.Sprintf("S%015d", newStream("finuance-sim/v1 ref", p.Slug, d.String(), fmt.Sprint(len(rows))).uint64()%1e15)
		rows = append(rows, r)
	}
	month := fmt.Sprintf("%02d/%04d", int(d.M), d.Y)
	if d.D == pr.payday {
		add(Row{Account: Current, Payee: pr.employer, Address: "UL. FIRMOWA 1, WARSZAWA", Title: "WYNAGRODZENIE " + month,
			Amount: pr.salary + int64(s.intn(40_001)), Kind: incoming, Category: "Wynagrodzenie"})
	}
	if d.D == pr.rentDay {
		add(Row{Account: Current, Payee: pr.landlord, Title: "CZYNSZ " + month, Amount: -pr.rent, Kind: outgoing, Category: "Mieszkanie"})
	}
	if d.D == pr.payday+1 { // part of the salary to savings, on both statements
		title := "OSZCZĘDNOŚCI " + month
		add(Row{Account: Current, Payee: "PRZELEW WŁASNY", Title: title, Amount: -pr.saving, Kind: ownOut, Category: "Przelew własny", Counterparty: sav})
		add(Row{Account: Savings, Payee: "PRZELEW WŁASNY", Title: title, Amount: pr.saving, Kind: ownIn, Category: "Przelew własny", Counterparty: cur})
	}
	for _, sub := range pr.subs {
		if d.D == sub.day {
			add(Row{Account: Current, Payee: sub.payee, Title: sub.title, Amount: sub.amount, Kind: sub.kind, Category: sub.category})
		}
	}
	if d.D == 15 {
		add(Row{Account: Current, Payee: "DOSTAWCA ENERGII ZIELONA MOC", Title: "PRĄD " + month, Amount: -(15_000 + s.skewed(0, 15_000)),
			Kind: directDebit, Category: "Rachunki"})
	}
	for range h.Density {
		for _, c := range everyday {
			if !s.permille(c.p * pr.spend / 100) {
				continue
			}
			payee := pick(s, c.payees)
			if h.Variety > 1 {
				i := s.intn(len(c.payees) * h.Variety)
				payee = shop(c.payees[i%len(c.payees)], i/len(c.payees)+1)
			}
			amount := s.skewed(c.lo, c.lo+(c.hi-c.lo)*int64(h.Spread)/100)
			r := Row{Account: Current, Payee: payee, Title: "ZAKUP", Amount: -amount, Kind: c.kind, Category: c.category}
			if c.kind == card && s.permille(300) {
				r.Value = platform.DateOf(d.Time().AddDate(0, 0, -1), time.UTC)
			}
			add(r)
		}
	}
	if d.D == 20 && s.permille(250) { // now and then money comes back from savings
		amount := 20_000 + int64(s.intn(9))*10_000
		title := "ZWROT Z OSZCZĘDNOŚCI " + month
		add(Row{Account: Savings, Payee: "PRZELEW WŁASNY", Title: title, Amount: -amount, Kind: ownOut, Category: "Przelew własny", Counterparty: cur})
		add(Row{Account: Current, Payee: "PRZELEW WŁASNY", Title: title, Amount: amount, Kind: ownIn, Category: "Przelew własny", Counterparty: sav})
	}
	if next := d.Time().AddDate(0, 0, 1); next.Day() == 1 {
		add(Row{Account: Savings, Payee: "BANK", Title: "KAPITALIZACJA ODSETEK", Amount: s.skewed(100, 4_000), Kind: interest, Category: "Odsetki"})
	}
	return rows
}

// Rows returns the rows of one account for the days from … to (inclusive), newest first; within a
// day the last drawn row comes first, as a bank lists the latest operation on top.
func Rows(p identity.Persona, account int, from, to platform.Date) []Row {
	return RowsWith(p, account, from, to, History{})
}

// RowsWith is Rows under a history profile.
func RowsWith(p identity.Persona, account int, from, to platform.Date, h History) []Row {
	var out []Row
	for t := to.Time(); !t.Before(from.Time()); t = t.AddDate(0, 0, -1) {
		day := DayWith(p, platform.DateOf(t, time.UTC), h)
		for i := len(day) - 1; i >= 0; i-- {
			if day[i].Account == account {
				out = append(out, day[i])
			}
		}
	}
	return out
}
