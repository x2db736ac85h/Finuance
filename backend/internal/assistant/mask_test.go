package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// TestMaskDigits: what identifies an account, a card, a phone or a person is cut down, written
// in one run or in groups; amounts, dates, short numbers and ordinary text are left alone (AI-2).
func TestMaskDigits(t *testing.T) {
	for in, want := range map[string]string{
		// the legacy rule: six or more digits in a row
		"PRZELEW 00000000000000000000000001": "PRZELEW 00…",
		"ref 123456":                         "ref 12…",
		"ref 12345":                          "ref 12345",
		// as banks and people write them
		"PL00 0000 0000 0000 0000 0000 0001":   "PL00…",
		"00 0000 0000 0000 0000 0000 0001":     "00…",
		"karta 4111-1111-1111-1111 zakup":      "karta 41… zakup",
		"karta 4111 1111 1111 1111":            "karta 41…",
		"tel. +48 600 700 800 po 16":           "tel. +48… po 16",
		"600-700-800":                          "60…",
		"BLIK na telefon 600700800":            "BLIK na telefon 60…",
		"FAKTURA dla jan.kowalski@example.com": "FAKTURA dla …@…",
		"a@b.example, c.d+e@mail.example.org":  "…@…, …@…",
		// not touched
		"-1 234,56 PLN":                    "-1 234,56 PLN",
		"1 234 567,89 PLN":                 "1 234 567,89 PLN",
		"2026-11-20":                       "2026-11-20",
		"20.11.2026 14:05":                 "20.11.2026 14:05",
		"ŻABKA Z1234 WARSZAWA":             "ŻABKA Z1234 WARSZAWA",
		"12 345":                           "12 345",
		"ul. Długa 12/34, 00-950 Warszawa": "ul. Długa 12/34, 00-950 Warszawa",
		"@ the market":                     "@ the market",
		"":                                 "",
	} {
		if got := MaskDigits(in); got != want {
			t.Errorf("MaskDigits(%q) = %q, want %q", in, got, want)
		}
	}
	// masking what is masked changes nothing
	for _, s := range []string{"PL00 0000 0000 0000 0000 0000 0001", "tel. +48 600 700 800", "x 12345678 y a@b.example"} {
		if once := MaskDigits(s); MaskDigits(once) != once {
			t.Errorf("masking twice changes %q → %q", once, MaskDigits(once))
		}
	}
}

// TestMaskJSONStrings: a stored tool result keeps its structure, numbers and ids, and loses the
// numbers and addresses inside its text.
func TestMaskJSONStrings(t *testing.T) {
	raw := `{"matched":1234567,"rows":[{"id":"0193e5a0-1111-7000-8000-123456789012","date":"2026-11-20","amount":"-1 234,56 PLN",` +
		`"payee":"JAN KOWALSKI 00 0000 0000 0000 0000 0000 0001","title":"zwrot \"tel\" 600700800, jan@example.com","n":987654321}]}`
	got := maskJSONStrings(raw)
	var v map[string]any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatalf("not JSON any more: %v\n%s", err, got)
	}
	row := v["rows"].([]any)[0].(map[string]any)
	if v["matched"] != float64(1234567) || row["n"] != float64(987654321) {
		t.Errorf("a number changed: %s", got)
	}
	if row["id"] != "0193e5a0-1111-7000-8000-123456789012" || row["date"] != "2026-11-20" || row["amount"] != "-1 234,56 PLN" {
		t.Errorf("an id, a date or an amount changed: %v", row)
	}
	if row["payee"] != "JAN KOWALSKI 00…" || row["title"] != `zwrot "tel" 60…, …@…` {
		t.Errorf("text not masked: %v", row)
	}
	for _, leak := range []string{"0000 0001", "600700800", "jan@example.com"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q is still in %s", leak, got)
		}
	}
	if got := maskJSONStrings("plain text 12345678"); got != "plain text 12…" {
		t.Errorf("plain text: %q", got)
	}
}

// TestMaskForCloud: earlier tool results and answers are masked, the user's own words are not.
func TestMaskForCloud(t *testing.T) {
	in := []llm.Message{
		{Role: llm.RoleUser, Content: "find my transfer to 00 0000 0000 0000 0000 0000 0001"},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_transactions"}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: `{"rows":[{"payee":"KONTO 00000000000000000000000001"}]}`},
		{Role: llm.RoleAssistant, Content: "It went to account 00000000000000000000000001."},
	}
	out := maskForCloud(in)
	if out[0].Content != in[0].Content {
		t.Errorf("the user's question was changed: %q", out[0].Content)
	}
	if out[2].Content != `{"rows":[{"payee":"KONTO 00…"}]}` || out[3].Content != "It went to account 00…." {
		t.Errorf("masked: %q / %q", out[2].Content, out[3].Content)
	}
	if out[1].ToolCalls[0].ID != "c1" || out[2].ToolCallID != "c1" {
		t.Error("the tool call lost its id")
	}
	if in[2].Content == out[2].Content {
		t.Error("the stored messages were changed in place")
	}
}

// TestCloudRunSendsNothingUnmasked: a chat that started on a local model and continues on a cloud
// one. The first answer's rows were stored as they are; the request to the cloud model carries
// them masked, with the system prompt's names, while the request to the local model had them in
// full (the control). The user's own new question goes as typed.
func TestCloudRunSendsNothingUnmasked(t *testing.T) {
	const account, phone, mail = "00000000000000000000000001", "600 700 800", "jan.kowalski@example.com"
	re := newRunEnv(t, 2)
	re.importFile(re.user, "Private", []byte("Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria\n"+
		"17.03.2025;17.03.2025;MASKTEST "+account+";;00000000000000000000000000;;tel "+phone+" "+mail+";-12,34;PLN;DEMO0000000002;PRZELEW;Inne\n"))
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{"cloud"}}); err != nil {
		t.Fatal(err)
	}
	chat := re.newChat()
	evs := re.ask(chat, `What did I pay at "MASKTEST"?`, nil) // the local model
	if got := names(evs); got != "reasoning,step,content,done" {
		t.Fatalf("local turn: %s", got)
	}
	local := string(re.mock.Requests()[len(re.mock.Requests())-1])
	if !strings.Contains(local, account) {
		t.Fatal("the local model did not get the account number: the control proves nothing")
	}
	before := len(re.mock.Requests())
	evs = re.ask(chat, "And who is that? My own number is 111222333.", func(r *Request) { r.Connection = "cloud" })
	if last(evs).Name != "done" {
		t.Fatalf("cloud turn: %s %+v", names(evs), last(evs).Data)
	}
	sent := re.mock.Requests()[before:]
	if len(sent) == 0 {
		t.Fatal("nothing was sent to the cloud model")
	}
	for i, req := range sent {
		body := string(req)
		for _, leak := range []string{account, account[:12], phone, mail, "jan.kowalski"} {
			if strings.Contains(body, leak) {
				t.Errorf("request %d to the cloud model carries %q", i+1, leak)
			}
		}
		if !strings.Contains(body, "MASKTEST 00…") {
			t.Errorf("request %d lost the masked row altogether", i+1)
		}
		if !strings.Contains(body, "My own number is 111222333.") {
			t.Errorf("request %d changed the user's own words", i+1)
		}
	}
}

// TestToolCallsPerStepAreCapped: a model that asks for thirty lookups in one step gets eight run;
// the rest of that step is dropped, and the turn still ends with an answer (AI-6).
func TestToolCallsPerStepAreCapped(t *testing.T) {
	re := newRunEnv(t, 2)
	calls := make([]llmmock.Call, 30)
	for i := range calls {
		calls[i] = llmmock.Call{Name: ToolSearch, Arguments: fmt.Sprintf(`{"query":"kino","limit":%d}`, i%5+1)}
	}
	re.mock.Push(llmmock.Reply{Calls: calls}, llmmock.Reply{Content: "done"})
	evs := re.ask(re.newChat(), "List everything, many times", nil)
	if last(evs).Name != "done" {
		t.Fatalf("events %s: %+v", names(evs), last(evs).Data)
	}
	steps := 0
	for _, e := range evs {
		if e.Name == "step" {
			steps++
		}
	}
	if steps != maxToolCalls {
		t.Errorf("%d lookups ran for a step that asked for %d, want %d", steps, len(calls), maxToolCalls)
	}
	// what went back to the model names exactly the calls that ran
	reqs := re.mock.Requests()
	if n := strings.Count(string(reqs[len(reqs)-1]), `"role":"tool"`); n != maxToolCalls {
		t.Errorf("%d tool results were sent back, want %d", n, maxToolCalls)
	}
}
