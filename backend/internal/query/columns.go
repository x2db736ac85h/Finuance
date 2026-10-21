package query

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/money"
)

// Type is a column's filter type (PLAN §4.5): it decides how a condition is read.
type Type int

// Column types.
const (
	Text Type = iota
	Number
	Date
)

func (t Type) String() string { return [...]string{"text", "number", "date"}[t] }

// Join names a table a column needs besides transactions (t) and accounts (a, always joined).
type Join string

// Optional joins.
const (
	JoinCategories Join = "categories" // c
	JoinGroups     Join = "groups"     // g
)

// Column is one filterable, sortable column. Every SQL string here is a constant written by us;
// the registry is the whitelist the compiler draws from.
type Column struct {
	Name  string // API name
	Type  Type
	Value string // typed value: what number/date conditions compare and what sorting orders by
	Text  string // display text: value checklist, `excluded`, export (amounts "-79,99", dates "dd.mm.yyyy")
	Norm  string // normalised text for the text fallback (Collapse(Norm(x)), as the _n columns)
	Join  Join
}

// The legacy shows amounts and dates as the file wrote them; the rebuild uses one fixed text form
// (Irwin's decision, Phase 7): comma decimals without grouping and dd.mm.yyyy. It equals the Pekao
// and demo files.
func dateText(expr string) string { return "to_char(" + expr + ", 'DD.MM.YYYY')" }

// scale is 10^exponent of the row's currency as a SQL expression. Every known currency has two
// decimals today, so it is the constant 100; a currency with another exponent turns it into a CASE.
func scale(currency string) string {
	other := money.NonDefaultExponents()
	pow := func(e int) string { return "1" + strings.Repeat("0", e) }
	if len(other) == 0 {
		return pow(money.DefaultExponent)
	}
	var b strings.Builder
	b.WriteString("CASE " + currency)
	for _, c := range slices.Sorted(maps.Keys(other)) {
		fmt.Fprintf(&b, " WHEN '%s' THEN %s", c, pow(other[c])) // ISO codes from our own table
	}
	b.WriteString(" ELSE " + pow(money.DefaultExponent) + " END")
	return b.String()
}

// major is a minor-unit amount as an exact numeric in major units (conditions compare in major
// units, as the legacy did with its parsed floats, so "> 100" means 100 PLN).
func major(minor, currency string) string {
	return "(" + minor + "::numeric / " + scale(currency) + ")"
}

// minorText formats a minor-unit amount as "-1234,56" (the exponent's digits after a comma).
func minorText(minor, currency string) string {
	sc := scale(currency)
	digits := "length(" + sc + "::text) - 1"
	return "(CASE WHEN " + minor + " < 0 THEN '-' ELSE '' END || (abs(" + minor + ") / " + sc + ")::text" +
		" || CASE WHEN " + sc + " = 1 THEN '' ELSE ',' || lpad((abs(" + minor + ") % " + sc + ")::text, " + digits + ", '0') END)"
}

func textCol(name, value, norm string, join Join) Column {
	return Column{Name: name, Type: Text, Value: value, Text: value, Norm: norm, Join: join}
}

func numberCol(name, minor string) Column {
	txt := minorText(minor, "t.currency")
	return Column{Name: name, Type: Number, Value: major(minor, "t.currency"), Text: txt, Norm: txt}
}

func dateCol(name, expr string) Column {
	return Column{Name: name, Type: Date, Value: expr, Text: dateText(expr), Norm: dateText(expr)}
}

// columns is the §4.5 whitelist, keyed by API name.
var columns = func() map[string]Column {
	list := []Column{
		dateCol("bookedOn", "t.booked_on"),
		dateCol("valueOn", "t.value_on"),
		numberCol("amount", "t.amount_minor"),
		numberCol("balanceAfter", "t.balance_after_minor"),
		textCol("currency", "t.currency::text", "lower(t.currency)", ""),
		textCol("payee", "t.payee", "t.payee_n", ""),
		textCol("description", "t.description", "t.description_n", ""),
		textCol("category", "c.name", "c.name_n", JoinCategories),
		textCol("bankCategory", "t.bank_category", "t.bank_category_n", ""),
		textCol("txType", "t.tx_type", "t.tx_type_n", ""),
		textCol("reference", "t.reference", "t.reference_n", ""),
		textCol("counterpartyAccount", "t.counterparty_account", "t.counterparty_n", ""),
		textCol("account", "a.name", "a.name_n", ""),
		textCol("group", "g.name", "g.name_n", JoinGroups),
		textCol("note", "t.note", "t.note_n", ""),
	}
	m := make(map[string]Column, len(list))
	for _, c := range list {
		m[c.Name] = c
	}
	return m
}()

// Lookup returns the registry entry of an API column name.
func Lookup(name string) (Column, bool) {
	c, ok := columns[name]
	return c, ok
}

// ColumnNames lists the filterable columns in a stable order (OpenAPI enum, tests).
func ColumnNames() []string { return slices.Sorted(maps.Keys(columns)) }

// sortExpr orders a column: text by the Polish natural collation with empties last, numbers and
// dates by value (NULLs last).
func (c Column) sortExpr(desc bool) string {
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	if c.Type == Text {
		return "NULLIF(" + c.Value + ", '') COLLATE pl_natural" + dir + " NULLS LAST"
	}
	return c.Value + dir + " NULLS LAST"
}

// placeholder numbers positional arguments.
type args struct{ vals []any }

func (a *args) add(v any) string {
	a.vals = append(a.vals, v)
	return "$" + strconv.Itoa(len(a.vals))
}
