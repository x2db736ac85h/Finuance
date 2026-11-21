package statements

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// pekaoHeader is the Pekao-like layout's header (internal/demo's, the same bank's look-alike).
const pekaoHeader = "Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria"

var pekaoKinds = map[kind]string{
	card: "TRANSAKCJA KARTĄ PŁATNICZĄ", blik: "PŁATNOŚĆ BLIK", incoming: "PRZELEW PRZYCHODZĄCY", outgoing: "PRZELEW MOBILE",
	ownOut: "PRZELEW WŁASNY", ownIn: "PRZELEW WŁASNY", interest: "KAPITALIZACJA ODSETEK", directDebit: "POLECENIE ZAPŁATY",
}

var mbankKinds = map[kind]string{
	card: "ZAKUP PRZY UŻYCIU KARTY", blik: "BLIK ZAKUP E-COMMERCE", incoming: "PRZELEW ZEWNĘTRZNY PRZYCHODZĄCY",
	outgoing: "PRZELEW ZEWNĘTRZNY WYCHODZĄCY", ownOut: "PRZELEW WŁASNY", ownIn: "PRZELEW WŁASNY",
	interest: "KAPITALIZACJA ODSETEK", directDebit: "POLECENIE ZAPŁATY",
}

// File is one account's statement for the days from … to (inclusive): its file name and bytes.
//
// stamp (letters, digits, '-' and '_'; anything else is dropped) makes the file byte-unique without
// changing a row: a non-empty stamp adds an "export"
// line before the table (the importer finds the header right above the first data row, so it
// reads the same rows). The API answers 409 to bytes it already holds; the simulator passes a new
// stamp for every upload, and the sample files have none.
func File(p identity.Persona, account int, from, to platform.Date, stamp string) (string, []byte) {
	stamp = strings.Map(func(r rune) rune { // letters, digits, '-' and '_' only: never a delimiter or a quote
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, stamp)
	rows := Rows(p, account, from, to)
	name := fmt.Sprintf("%s-%s-%s-%s.csv", p.Slug, strings.ToLower(strings.ReplaceAll(AccountName(account), " ", "-")), from, to)
	if LayoutOf(p, account) == MBankLike {
		return name, mbank(p, account, from, to, rows, stamp)
	}
	return name, pekao(p, account, rows, stamp)
}

func pekao(p identity.Persona, account int, rows []Row, stamp string) []byte {
	var b strings.Builder
	if stamp != "" {
		b.WriteString("Eksport;" + stamp + "\n")
	}
	b.WriteString(pekaoHeader + "\n")
	own := "'" + Number(p, account)
	for i := range rows {
		r := &rows[i]
		other := ""
		if r.Counterparty != "" {
			other = "'" + r.Counterparty
		}
		b.WriteString(strings.Join([]string{dmy(r.Booked), dmy(r.Value), r.Payee, r.Address, own, other, r.Title,
			amount(r.Amount), "PLN", "'" + r.Ref, pekaoKinds[r.Kind], r.Category}, ";"))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func mbank(p identity.Persona, account int, from, to platform.Date, rows []Row, stamp string) []byte {
	var in, out int64
	for i := range rows {
		if r := &rows[i]; r.Amount > 0 {
			in += r.Amount
		} else {
			out += r.Amount
		}
	}
	number := Number(p, account)
	short := "Konto " + number[:4] + " ... " + number[len(number)-4:]
	lines := []string{
		"\ufeffmBank Przykładowy S.A. Bankowość Detaliczna;",
		"\t\t\tSkrytka Pocztowa 0000;",
		"\t\t\t00-000 Miasto 2;",
		"\t\t\twww.example.pl;",
		"\t\t\tInfolinia: 000 000 000;",
		"\t\t\t+48 (00) 0 000 000;",
		"\t\t\t",
		"",
		"#Klient;",
		strings.ToUpper(p.Name) + " PRZYKŁADOWY;",
		"",
		"Lista operacji;",
		"",
		"#Za okres:;",
		dmy(from) + ";" + dmy(to) + ";",
		"",
	}
	if stamp != "" {
		lines = append(lines, "#Eksport:;"+stamp+";", "")
	}
	lines = append(lines,
		"#zgodnie z wybranymi filtrami wyszukiwania;",
		"      #dla rachunków:;",
		"      Konto - "+number+";",
		"      ",
		"      #Lista nie jest dokumentem w rozumieniu art. 7 Ustawy Prawo Bankowe, ponieważ operacje można samodzielnie edytować.;",
		"      ",
		"      #Waluta;#Wpływy;#Wydatki;",
		"PLN;"+amount(in)+";"+amount(out)+";",
		"      ",
		"#Data operacji;#Opis operacji;#Rachunek;#Kategoria;#Kwota;",
	)
	for i := range rows {
		r := &rows[i]
		desc := r.Payee + "  " + mbankKinds[r.Kind] + "  " + r.Title
		if r.Counterparty != "" {
			desc += "  " + r.Counterparty
		}
		lines = append(lines, r.Booked.String()+`;"`+desc+`";"`+short+`";"`+r.Category+`";`+amount(r.Amount)+" PLN;;")
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// dmy writes a date as 31.01.2026.
func dmy(d platform.Date) string { return fmt.Sprintf("%02d.%02d.%04d", d.D, int(d.M), d.Y) }

// amount writes grosze as the banks do: a comma before the grosze and a space between thousands
// (-2 132,22).
func amount(g int64) string {
	sign := ""
	if g < 0 {
		sign, g = "-", -g
	}
	zl := strconv.FormatInt(g/100, 10)
	for i := len(zl) - 3; i > 0; i -= 3 {
		zl = zl[:i] + " " + zl[i:]
	}
	return fmt.Sprintf("%s%s,%02d", sign, zl, g%100)
}
