package assistant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Digest schedules (ADR 0033).
const (
	DigestOff    = "off"
	DigestDaily  = "daily"
	DigestWeekly = "weekly"
)

// Chat kinds.
const (
	KindChat   = "chat"
	KindDigest = "digest"
)

// DigestSettings is a user's digest schedule: off, daily, or weekly on an ISO weekday (1 = Monday),
// at an hour of the time zone, written with the connection and model named ("" = the default).
type DigestSettings struct {
	Schedule   string
	Hour       int
	Weekday    int
	TimeZone   string
	Connection string
	Model      string
}

// DefaultDigest is the schedule before the user picks one: off; weekly on Monday at 07:00 when
// switched on without more.
func DefaultDigest() DigestSettings {
	return DigestSettings{Schedule: DigestOff, Hour: 7, Weekday: 1, TimeZone: DefaultTimeZone}
}

func (d DigestSettings) validate(ve *platform.ValidationError) {
	switch d.Schedule {
	case DigestOff, DigestDaily, DigestWeekly:
	default:
		ve.Add("digest.schedule", "must be off, daily or weekly")
	}
	if d.Hour < 0 || d.Hour > 23 {
		ve.Add("digest.hour", "must be 0 to 23")
	}
	if d.Weekday < 1 || d.Weekday > 7 {
		ve.Add("digest.weekday", "must be 1 (Monday) to 7 (Sunday)")
	}
	if _, err := time.LoadLocation(d.TimeZone); err != nil || d.TimeZone == "" || len(d.TimeZone) > 64 {
		ve.Add("digest.timeZone", "must be a time zone such as Europe/Warsaw")
	}
	if len(d.Connection) > 40 || len(d.Model) > 200 {
		ve.Add("digest.model", "is too long")
	}
}

// NextDigest is the first time after `after` a digest is due: the hour on the next day (daily) or
// the next such weekday (weekly), in the schedule's time zone. False when digests are off. An hour
// that a daylight-saving change skips becomes the hour after it.
func NextDigest(d DigestSettings, after time.Time) (time.Time, bool) {
	if d.Schedule != DigestDaily && d.Schedule != DigestWeekly {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(d.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	local := after.In(loc)
	for i := range 8 {
		day := time.Date(local.Year(), local.Month(), local.Day()+i, d.Hour, 0, 0, 0, loc)
		if !day.After(after) {
			continue
		}
		if d.Schedule == DigestWeekly && isoWeekday(day) != d.Weekday {
			continue
		}
		return day, true
	}
	return time.Time{}, false // unreachable: a week always has the weekday
}

func isoWeekday(t time.Time) int {
	if w := int(t.Weekday()); w != 0 {
		return w
	}
	return 7
}

// digestQuestion is what a digest asks: fixed, English (the answer follows the question's
// language), naming the period in the user's time zone.
func digestQuestion(schedule string, now time.Time) (title, text string) {
	const ask = "Answer in at most 8 short bullet points, the most important first, with amounts. Where a change would " +
		"clearly help (a budget, a goal, a category or a rule), propose it with propose_action; never say you changed " +
		"anything, because proposals wait for me to apply them. If there is nothing worth saying, say so in one line."
	if schedule == DigestWeekly {
		end := time.Date(now.Year(), now.Month(), now.Day()-isoWeekday(now), 0, 0, 0, 0, now.Location()) // last Sunday
		start := end.AddDate(0, 0, -6)
		return "Weekly digest · " + now.Format("2 Jan"),
			fmt.Sprintf("Write my weekly digest for last week (%s to %s). Look up last week's spending by category compared "+
				"with the weeks before it, this month's budgets that are over or ahead of pace, how my goals are doing, and "+
				"recurring payments due in the next 7 days. %s", start.Format("Mon 2 Jan 2006"), end.Format("Mon 2 Jan 2006"), ask)
	}
	yesterday := now.AddDate(0, 0, -1)
	return "Daily digest · " + now.Format("2 Jan"),
		fmt.Sprintf("Write my daily digest for today, %s. Look up what I spent yesterday (%s) and anything unusual in it, "+
			"this month's budgets that are over or ahead of pace, how my goals are doing, and recurring payments due in "+
			"the next 7 days. %s", now.Format("Mon 2 Jan 2006"), yesterday.Format("2 Jan"), ask)
}

// DigestPolicy is what the instance allows one user's digest: Allowed is false for a disabled or
// demo user, or when an administrator switched digests (or the assistant) off.
type DigestPolicy struct {
	Allowed      bool
	TokensPerDay int64 // 0 = no limit
}

// Notifier sends in-app notifications (notify.Service).
type Notifier interface {
	Send(ctx context.Context, userID uuid.UUID, kind string, data map[string]string) error
}

// NotifyDigest is the notification kind of a finished digest (notify.AssistantDigest).
const NotifyDigest = "assistant.digest"

// DigestsConfig wires Digests.
type DigestsConfig struct {
	Assistant *Assistant
	Policy    func(ctx context.Context, userID uuid.UUID) (DigestPolicy, error)
	Notify    Notifier
	Clock     platform.Clock
	Logger    *slog.Logger
}

// Digests schedules and writes digests (the worker's jobs, ADR 0033).
type Digests struct {
	cfg DigestsConfig
}

// NewDigests returns the digest service.
func NewDigests(cfg DigestsConfig) *Digests {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	return &Digests{cfg: cfg}
}

const scanPage = 100

// Schedule takes every user whose digest is due, moves the due time to the next one and calls
// enqueue in the same transaction, so a digest is queued exactly when its due time moves. It
// returns how many were queued.
func (d *Digests) Schedule(ctx context.Context, enqueue func(ctx context.Context, tx pgx.Tx, userID uuid.UUID, due time.Time) error) (int, error) {
	chats := d.cfg.Assistant.Chats
	queued := 0
	for range 100 { // each round moves every listed user on, so the next one lists new ones
		now := d.cfg.Clock.Now()
		var due []db.DueDigestUsersRow
		if err := chats.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
			due, err = q.DueDigestUsers(ctx, db.DueDigestUsersParams{Now: &now, PageSize: scanPage})
			return err
		}); err != nil {
			return queued, err
		}
		for _, u := range due {
			if err := ctx.Err(); err != nil {
				return queued, err
			}
			err := chats.st.WithUser(ctx, u.ID, func(q *db.Queries, tx pgx.Tx) error {
				s, err := settings(ctx, q, u.ID)
				if err != nil {
					return err
				}
				next, on := NextDigest(*s.Digest, now)
				if err := q.SetDigestDueAt(ctx, db.SetDigestDueAtParams{ID: u.ID, DueAt: timePtr(next, on)}); err != nil {
					return err
				}
				if !on {
					return nil
				}
				queued++
				return enqueue(ctx, tx, u.ID, u.DueAt)
			})
			if err != nil {
				return queued, err
			}
		}
		if len(due) < scanPage {
			return queued, nil
		}
	}
	return queued, nil
}

func timePtr(t time.Time, ok bool) *time.Time {
	if !ok {
		return nil
	}
	return &t
}

// DigestOutcome is what one Write did.
type DigestOutcome struct {
	ChatID    uuid.UUID
	Skipped   string // why nothing was written ("" when a digest was)
	Failed    bool
	Proposals int
}

// Write writes a digest for the user now: a new chat of kind digest with the fixed question,
// answered through the normal run loop with the read-only tools, then a notification. A scheduled
// digest is skipped when the user switched digests off since; manual asks for one whatever the
// schedule. Nothing is written when the instance doesn't allow it or the day's tokens are used.
func (d *Digests) Write(ctx context.Context, userID uuid.UUID, manual bool) (DigestOutcome, error) {
	a := d.cfg.Assistant
	if !a.Connections.Enabled() {
		return DigestOutcome{Skipped: "no connection"}, nil
	}
	pol, err := d.cfg.Policy(ctx, userID)
	if err != nil {
		return DigestOutcome{}, err
	}
	if !pol.Allowed {
		return DigestOutcome{Skipped: "not allowed"}, nil
	}
	s, err := a.Chats.Settings(ctx, userID)
	if err != nil {
		return DigestOutcome{}, err
	}
	ds := *s.Digest
	if ds.Schedule == DigestOff && !manual {
		return DigestOutcome{Skipped: "off"}, nil
	}
	if pol.TokensPerDay > 0 && s.TokensToday >= pol.TokensPerDay {
		return DigestOutcome{Skipped: "token budget"}, nil
	}
	kind := ds.Schedule
	if kind == DigestOff {
		kind = DigestDaily
	}
	title, text := digestQuestion(kind, UserTime(d.cfg.Clock.Now(), ds.TimeZone))
	chat, err := a.Chats.create(ctx, userID, KindDigest, title)
	if err != nil {
		return DigestOutcome{}, err
	}
	out := DigestOutcome{ChatID: chat.ID}
	req := Request{ChatID: chat.ID, Text: text, Connection: ds.Connection, Model: ds.Model, Mode: ModeAuto, Think: llm.ThinkAuto,
		TimeZone: ds.TimeZone, Toolset: ToolsetFull, TokensPerDay: pol.TokensPerDay, Digest: true}
	if _, err := a.Connections.Get(req.Connection); err != nil {
		req.Connection, req.Model = "", "" // the connection was removed since: the default
	}
	run, err := a.Runner.Prepare(ctx, userID, req)
	if err != nil {
		if derr := a.Chats.Delete(ctx, userID, chat.ID); derr != nil {
			return out, errors.Join(err, derr)
		}
		var le *instance.LimitError
		if errors.As(err, &le) {
			return DigestOutcome{Skipped: "token budget"}, nil
		}
		d.cfg.Logger.WarnContext(ctx, "assistant digest not started", "error_kind", startErrorKind(err))
		out.ChatID, out.Failed = uuid.Nil, true
		return out, d.notify(ctx, userID, kind, out)
	}
	run.Execute(ctx, func(e Event) {
		switch v := e.Data.(type) {
		case StepEvent:
			if v.Proposal != nil {
				out.Proposals++
			}
		case ErrorEvent:
			out.Failed = true
		}
	})
	return out, d.notify(ctx, userID, kind, out)
}

func (d *Digests) notify(ctx context.Context, userID uuid.UUID, schedule string, o DigestOutcome) error {
	data := map[string]string{"schedule": schedule, "proposals": strconv.Itoa(o.Proposals)}
	if o.ChatID != uuid.Nil {
		data["chatId"] = o.ChatID.String()
	}
	if o.Failed {
		data["failed"] = "true"
	}
	return d.cfg.Notify.Send(ctx, userID, NotifyDigest, data)
}

// startErrorKind names why a run couldn't start, for the log (never the user's data).
func startErrorKind(err error) string {
	switch {
	case errors.Is(err, ErrConsent):
		return "consent_required"
	case errors.Is(err, ErrBusy), errors.Is(err, ErrRunning):
		return "busy"
	case errors.Is(err, ErrUnknownConnection), errors.Is(err, ErrUnknownModel):
		return "model_not_found"
	}
	var le *llm.Error
	if errors.As(err, &le) {
		return string(le.Kind)
	}
	return "internal"
}
