package main

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	unorm "golang.org/x/text/unicode/norm"
)

var stripMarks = transform.Chain(unorm.NFD, runes.Remove(runes.In(unicode.Mn)))

// norm ports the legacy norm(): lowercase, NFD, drop combining marks, ł → l.
func norm(s string) string {
	out, _, err := transform.String(stripMarks, strings.ToLower(s))
	if err != nil {
		out = strings.ToLower(s)
	}
	return strings.ReplaceAll(out, "ł", "l")
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// allowed holds banking vocabulary (normalised) that free text keeps verbatim. Words of one or two
// letters are always kept. Anything else is replaced, so err on the side of a short list.
var allowed = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		blik ref przelew przelewy przelewem transakcja transakcji transakcje karta karty karte karta platnosc platnosci
		platnicza platniczej platniczy zakup zakupy zakupu uzyciu przy wyplata wyplaty wplata wplaty wlasny wlasna atm
		oplata oplaty oplate miesieczna miesieczny za uzytkowanie mobile przychodzacy przychodzace przychodzaca
		wychodzacy wychodzace wychodzaca zewnetrzny zewnetrzne wewnetrzny wewnetrzne internet internetowy internetowa
		commerce nierozliczona nierozliczone rozliczona tytul tytulem lokalizacja adres miasto kraj polska poland
		data wykonania operacji operacja operacje kwota kwoty cash back oryginalna oryginalny numer rachunek rachunku
		rachunkow rachunki nadawcy odbiorcy nadawca odbiorca waluta waluty wplywy wydatki klient klienta okres lista
		zgodnie wybranymi filtrami wyszukiwania dla nie jest dokumentem rozumieniu art ustawy prawo bankowe poniewaz
		mozna samodzielnie edytowac pozniejszymi zmianami roku poz saldo koncowe poczatkowe pln eur usd gbp chf
		vat faktura fvt darowizna cele statutowe statutowa ubezpieczenie prowizja odsetki kapitalizacja zwrot
		srodkow zlecenie stale polecenie zaplaty splata kredytu rata bankomat bankomatu wplatomat spolka ograniczona
		ogran odpowiedzialnoscia akcyjna komandytowa mtransfer express elixir sorbnet swift sepa iban bic rfb opt
		transfer payment card purchase fee withdrawal deposit salary wynagrodzenie czynsz kategorii bez inne bank
		bankowosc detaliczna skrytka pocztowa infolinia mlinia www uznanie obciazenie wplyw wydatek online line
		tel telefon pending debit credit balance amount date description title currency category
		zlotych grosze opis typ rodzaj referencyjny referencja bankowy bankowa przelewu zakupu wyplaty
		konto konta wyciag raport wygenerowano strona eksport export statement account
		bankowa bankowosci elektronicznej infolinia uslugi`) {
		m[w] = true
	}
	return m
}()

var merchants = []string{
	"SKLEP ALFA", "SKLEP BETA", "MARKET GAMMA", "MARKET DELTA", "KAWIARNIA OMEGA", "KAWIARNIA SIGMA",
	"PIEKARNIA ZORZA", "PIEKARNIA LIPA", "APTEKA POLNA", "APTEKA KLON", "STACJA PALIW SOKOL", "STACJA PALIW ORZEL",
	"BISTRO WIATR", "BISTRO RZEKA", "KSIEGARNIA SOWA", "SALON KORAL", "SERWIS KOMETA", "STUDIO ZEFIR",
	"HURTOWNIA DAB", "DELIKATESY JASMIN", "RESTAURACJA MOST", "KINO ZIELONE", "TEATR PROMYK", "SKLEP INTERNETOWY NOVA",
	"PLATNOSCI ONLINE SA", "OPERATOR TELEKOM", "DOSTAWCA ENERGII", "WSPOLNOTA OSIEDLE", "KLUB FITNESS RUCH",
	"PRZEWOZNIK MIEJSKI", "FIRMA PRZYKLADOWA", "USLUGI CYFROWE", "SKLEP SPORTOWY START", "OGRODNIK ZIELEN",
	"PIZZERIA SLONCE", "BAR SZYBKI", "OPTYK WIDOK", "WETERYNARZ LAPA", "KWIACIARNIA ROZA", "DROGERIA PIANKA",
}

// Invented place names: a real city could coincide with the user's own and survive anonymisation.
var cities = []string{
	"MIASTKOWO", "ZIELONKA DOLNA", "KAMIENIEC MALY", "BRZOZOWICE", "LIPNIKI", "SOSNOWA WOLA", "DABROWKA NOWA",
	"JEZIORNO", "GORKI WIELKIE", "WIERZBNO", "OLSZYNKA", "KLONOWO", "BUKOWIEC GORNY", "JASIONOWO", "MODRZEWICE",
	"PAPROTNIA", "KRZEWINY", "LESNICZOWKA", "BOROWINA", "POTOKOWO",
}
