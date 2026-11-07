package assistant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

func TestNextDigest(t *testing.T) {
	waw, _ := time.LoadLocation("Europe/Warsaw")
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, waw) }
	daily := DigestSettings{Schedule: DigestDaily, Hour: 7, Weekday: 1, TimeZone: "Europe/Warsaw"}
	weekly := DigestSettings{Schedule: DigestWeekly, Hour: 7, Weekday: 1, TimeZone: "Europe/Warsaw"}
	for _, c := range []struct {
		name  string
		d     DigestSettings
		after time.Time
		want  time.Time
	}{
		{"daily, before the hour", daily, at(2026, 11, 6, 6, 59), at(2026, 11, 6, 7, 0)},
		{"daily, on the hour: the next day", daily, at(2026, 11, 6, 7, 0), at(2026, 11, 7, 7, 0)},
		{"daily, across the month", daily, at(2026, 11, 30, 23, 0), at(2026, 12, 1, 7, 0)},
		{"weekly from a Friday: Monday", weekly, at(2026, 11, 6, 12, 0), at(2026, 11, 9, 7, 0)},
		{"weekly on Monday after the hour: a week later", weekly, at(2026, 11, 9, 8, 0), at(2026, 11, 16, 7, 0)},
		{"weekly on Sunday", DigestSettings{Schedule: DigestWeekly, Hour: 20, Weekday: 7, TimeZone: "Europe/Warsaw"},
			at(2026, 11, 6, 12, 0), at(2026, 11, 8, 20, 0)},
		// 29 March 2026: 02:00 doesn't exist in Warsaw (summer time starts), so 03:00 local
		{"an hour summer time skips", DigestSettings{Schedule: DigestDaily, Hour: 2, Weekday: 1, TimeZone: "Europe/Warsaw"},
			at(2026, 3, 28, 12, 0), time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)},
		// 25 October 2026: the day is 25 hours long, the digest still comes at 07:00 local
		{"across the end of summer time", daily, at(2026, 10, 24, 8, 0), time.Date(2026, 10, 25, 6, 0, 0, 0, time.UTC)},
		{"another zone", DigestSettings{Schedule: DigestDaily, Hour: 7, Weekday: 1, TimeZone: "America/New_York"},
			time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)},
	} {
		got, ok := NextDigest(c.d, c.after)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, got, ok, c.want)
		}
	}
	if _, ok := NextDigest(DefaultDigest(), at(2026, 11, 6, 6, 0)); ok {
		t.Error("off has a next digest")
	}
}

func TestDigestQuestion(t *testing.T) {
	waw, _ := time.LoadLocation("Europe/Warsaw")
	now := time.Date(2026, 11, 9, 7, 0, 0, 0, waw) // a Monday
	title, text := digestQuestion(DigestWeekly, now)
	if title != "Weekly digest · 9 Nov" || !strings.Contains(text, "last week (Mon 2 Nov 2026 to Sun 8 Nov 2026)") ||
		!strings.Contains(text, "propose_action") {
		t.Errorf("weekly = %q / %q", title, text)
	}
	title, text = digestQuestion(DigestDaily, now)
	if title != "Daily digest · 9 Nov" || !strings.Contains(text, "today, Mon 9 Nov 2026") || !strings.Contains(text, "yesterday (8 Nov)") {
		t.Errorf("daily = %q / %q", title, text)
	}
}

func TestDigestSettingsValidation(t *testing.T) {
	re := newRunEnv(t, 1)
	_, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Digest: &DigestSettings{Schedule: "hourly",
		Hour: 24, Weekday: 0, TimeZone: "Mars/Olympus"}})
	var ve *platform.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %v", err)
	}
	for _, f := range []string{"digest.schedule", "digest.hour", "digest.weekday", "digest.timeZone"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("no %s in %v", f, err)
		}
	}
}

type sentNote struct {
	user uuid.UUID
	kind string
	data map[string]string
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sentNote
}

func (f *fakeNotifier) Send(_ context.Context, u uuid.UUID, kind string, data map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentNote{u, kind, data})
	return nil
}

// TestDigests: a schedule sets the due time; the scan queues a digest and moves it on; writing one
// answers the fixed question in a digest chat with its proposal waiting, counts the tokens and
// notifies; switched off, not allowed or over the budget, nothing is written (ADR 0033).
func TestDigests(t *testing.T) {
	re := newRunEnv(t, 1)
	re.clock.t = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC) // a Friday, 11:00 in Warsaw
	notes := &fakeNotifier{}
	policy := DigestPolicy{Allowed: true, TokensPerDay: 200_000}
	ai := &Assistant{Connections: re.runner.cfg.Connections, Chats: re.chats, Tools: re.runner.cfg.Tools, Runner: re.runner}
	d := NewDigests(DigestsConfig{Assistant: ai, Notify: notes, Clock: re.clock,
		Policy: func(context.Context, uuid.UUID) (DigestPolicy, error) { return policy, nil }})

	weekly := DigestSettings{Schedule: DigestWeekly, Hour: 7, Weekday: 1, TimeZone: "Europe/Warsaw", Connection: "mock", Model: "mock-tools"}
	s, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Digest: &weekly})
	monday := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC) // 07:00 in Warsaw
	if err != nil || s.DigestNextAt == nil || !s.DigestNextAt.Equal(monday) || *s.Digest != weekly {
		t.Fatalf("settings = %+v %v", s, err)
	}
	// saving without a digest keeps the schedule and its due time
	if s, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}}); err != nil || *s.Digest != weekly ||
		!s.DigestNextAt.Equal(monday) {
		t.Fatalf("kept = %+v %v", s, err)
	}

	type queued struct {
		user uuid.UUID
		due  time.Time
	}
	var got []queued
	enqueue := func(_ context.Context, _ pgx.Tx, u uuid.UUID, due time.Time) error {
		got = append(got, queued{u, due})
		return nil
	}
	mine := func() []queued {
		var out []queued
		for _, q := range got {
			if q.user == re.user {
				out = append(out, q)
			}
		}
		return out
	}
	if _, err := d.Schedule(re.ctx, enqueue); err != nil || len(mine()) != 0 {
		t.Fatalf("not due yet: %v %v", err, mine())
	}
	re.clock.t = monday.Add(5 * time.Minute)
	if _, err := d.Schedule(re.ctx, enqueue); err != nil || len(mine()) != 1 || !mine()[0].due.Equal(monday) {
		t.Fatalf("due: %v %v", err, mine())
	}
	if s, _ := re.chats.Settings(re.ctx, re.user); !s.DigestNextAt.Equal(monday.AddDate(0, 0, 7)) {
		t.Errorf("next = %v", s.DigestNextAt)
	}
	if _, err := d.Schedule(re.ctx, enqueue); err != nil || len(mine()) != 1 {
		t.Errorf("queued twice: %v %v", err, mine())
	}

	re.mock.Push(llmmock.Reply{Calls: []llmmock.Call{{Name: ToolPropose,
		Arguments: `{"action":"set_budget","to_category":"Restauracje i kawiarnie","amount":300,"reason":"over budget twice"}`}}},
		llmmock.Reply{Content: "- Eating out is over budget."})
	out, err := d.Write(re.ctx, re.user, false)
	if err != nil || out.Skipped != "" || out.Failed || out.Proposals != 1 {
		t.Fatalf("write = %+v %v", out, err)
	}
	chat, msgs, err := re.chats.Get(re.ctx, re.user, out.ChatID)
	if err != nil || chat.Kind != KindDigest || chat.Title != "Weekly digest · 9 Nov" || !strings.HasPrefix(msgs[0].Content, "Write my weekly digest") ||
		msgs[len(msgs)-1].Content != "- Eating out is over budget." {
		t.Fatalf("digest chat = %+v %v", chat, err)
	}
	if req := re.mock.Requests(); len(req) == 0 || !strings.Contains(string(req[len(req)-1]), `"model":"mock-tools"`) {
		t.Errorf("model = %s", req[len(req)-1])
	}
	if len(notes.sent) != 1 || notes.sent[0].kind != NotifyDigest || notes.sent[0].data["chatId"] != out.ChatID.String() ||
		notes.sent[0].data["proposals"] != "1" || notes.sent[0].data["schedule"] != "weekly" || notes.sent[0].data["failed"] != "" {
		t.Errorf("notifications = %+v", notes.sent)
	}
	// the run's tokens, as the server counted them (the mock: 100 in and 20 out per request), are the day's
	if s, _ := re.chats.Settings(re.ctx, re.user); s.TokensToday != 240 {
		t.Errorf("tokens today = %d", s.TokensToday)
	}
	var digests int64
	if err := re.st.WithUser(re.ctx, re.user, func(q *db.Queries, _ pgx.Tx) error {
		u, err := q.GetAssistantUsage(re.ctx, db.GetAssistantUsageParams{UserID: re.user, Day: utcDay(re.clock.Now())})
		digests = u.Digests
		return err
	}); err != nil || digests != 1 {
		t.Errorf("digests counted = %d %v", digests, err)
	}

	// a question can't start once the day's tokens are used; neither can a digest
	policy.TokensPerDay = 240
	if out, err := d.Write(re.ctx, re.user, true); err != nil || out.Skipped != "token budget" {
		t.Errorf("over budget: %+v %v", out, err)
	}
	var le *instance.LimitError
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: re.newChat(), Text: "Hi", TokensPerDay: 240}); !errors.As(err, &le) ||
		le.Limit != "assistantTokensPerDay" {
		t.Errorf("question over budget: %v", err)
	}
	policy.TokensPerDay = 0

	// not allowed (switched off, demo or disabled user), or switched off by the user since
	policy.Allowed = false
	if out, err := d.Write(re.ctx, re.user, true); err != nil || out.Skipped != "not allowed" {
		t.Errorf("not allowed: %+v %v", out, err)
	}
	policy.Allowed = true
	off := DefaultDigest()
	if s, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Digest: &off}); err != nil || s.DigestNextAt != nil {
		t.Fatalf("off = %+v %v", s, err)
	}
	if out, err := d.Write(re.ctx, re.user, false); err != nil || out.Skipped != "off" {
		t.Errorf("scheduled while off: %+v %v", out, err)
	}

	// one asked for now is written whatever the schedule (daily wording); a model error says so
	boom := llmmock.Reply{Status: 500, Body: `{"error":{"message":"boom"}}`}
	re.mock.Push(boom, boom) // auto mode tries plan mode after the first
	out, err = d.Write(re.ctx, re.user, true)
	if err != nil || !out.Failed || len(notes.sent) != 2 || notes.sent[1].data["failed"] != "true" || notes.sent[1].data["schedule"] != "daily" {
		t.Errorf("failed digest = %+v %v %+v", out, err, notes.sent)
	}
	// a removed connection falls back to the default; a cloud one without consent isn't used
	cloud := DigestSettings{Schedule: DigestDaily, Hour: 7, Weekday: 1, TimeZone: "Europe/Warsaw", Connection: "cloud"}
	if _, err := re.chats.SaveSettings(re.ctx, re.user, Settings{CloudConsent: []string{}, Digest: &cloud}); err != nil {
		t.Fatal(err)
	}
	if out, err := d.Write(re.ctx, re.user, true); err != nil || !out.Failed || out.ChatID != uuid.Nil {
		t.Errorf("cloud without consent: %+v %v", out, err)
	}
	if list, _ := re.chats.List(re.ctx, re.user); len(list) != 3 { // the weekly, the failed one and the question's chat
		t.Errorf("%d chats (a digest that couldn't start leaves none)", len(list))
	}
}
