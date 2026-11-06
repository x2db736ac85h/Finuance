package goals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Service stores goals and works out their progress; it is safe for concurrent use.
type Service struct {
	st    *store.Store
	clock platform.Clock
}

// New returns the service.
func New(st *store.Store, clock platform.Clock) *Service { return &Service{st: st, clock: clock} }

// Limits.
const (
	MaxGoals    = 50
	maxName     = 80
	maxYears    = 10
	StatusOpen  = "active"
	StatusDone  = "done"
	StatusShelf = "archived"
)

// Goal is a stored goal with the names of its category and account.
type Goal struct {
	ID             uuid.UUID
	Name           string
	Kind           string
	TargetMinor    *int64
	Currency       string
	AccountID      *uuid.UUID
	AccountName    string
	CategoryID     *uuid.UUID
	CategoryName   string
	ReducePct      *int
	BaselineMonths int
	StartsOn       platform.Date
	Deadline       platform.Date
	Status         string
	Version        int
	AI             bool // an assistant change set it and nobody undid it (ADR 0030)
	Progress       Progress
}

// Input is a goal as created or changed. Kind, Currency and StartsOn are read on create only.
type Input struct {
	Name           string
	Kind           string
	TargetMinor    *int64
	Currency       string // save without an account: default the first account's
	AccountID      *uuid.UUID
	CategoryID     *uuid.UUID
	ReducePct      *int
	BaselineMonths int // default 3
	StartsOn       *platform.Date
	Deadline       platform.Date
	Status         string // update only; default active
	Version        int    // update only: the version read
}

// State is a goal in an AI change (ADR 0030): what an update sets, or Created for a new goal.
type State struct {
	Created        bool          `json:"created,omitempty"`
	Name           string        `json:"name"`
	TargetMinor    *int64        `json:"targetMinor,omitempty"`
	AccountID      *uuid.UUID    `json:"accountId,omitempty"`
	CategoryID     *uuid.UUID    `json:"categoryId,omitempty"`
	ReducePct      *int16        `json:"reducePct,omitempty"`
	BaselineMonths int16         `json:"baselineMonths"`
	Deadline       platform.Date `json:"deadline"`
	Status         string        `json:"status"`
	Version        int32         `json:"version"`
}

// StateOf is a stored goal's State.
func StateOf(g *db.Goal) State {
	return State{Name: g.Name, TargetMinor: g.TargetMinor, AccountID: g.AccountID, CategoryID: g.CategoryID, ReducePct: g.ReducePct,
		BaselineMonths: g.BaselineMonths, Deadline: g.Deadline, Status: g.Status, Version: g.Version}
}

func invalidf(format string, a ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, a...), ledger.ErrInvalid)
}

// Today is the day progress is worked out for.
func (s *Service) Today() platform.Date { return platform.DateOf(s.clock.Now(), time.UTC) }

// clean checks an input; create says whether kind, currency and start apply.
func (s *Service) clean(in *Input, create bool) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > maxName:
		return invalidf("the name must be 1–%d characters", maxName)
	case create && in.Kind != KindSave && in.Kind != KindReduce:
		return invalidf("kind must be save or reduce")
	case in.BaselineMonths == 0:
		in.BaselineMonths = 3
	}
	if in.BaselineMonths < 1 || in.BaselineMonths > 12 {
		return invalidf("the baseline is 1–12 months")
	}
	if create && in.StartsOn == nil {
		t := s.Today()
		in.StartsOn = &t
	}
	if in.Status == "" {
		in.Status = StatusOpen
	}
	if in.Status != StatusOpen && in.Status != StatusDone && in.Status != StatusShelf {
		return invalidf("status must be active, done or archived")
	}
	return nil
}

func checkKind(kind string, in *Input, start platform.Date) error {
	switch {
	case !in.Deadline.After(start):
		return invalidf("the deadline must be after the start (%s)", start)
	case in.Deadline.After(start.AddMonths(12 * maxYears)):
		return invalidf("the deadline must be within %d years", maxYears)
	case kind == KindSave && (in.TargetMinor == nil || *in.TargetMinor <= 0):
		return invalidf("a savings goal needs a positive target")
	case kind == KindSave && in.CategoryID != nil:
		return invalidf("a savings goal has no category")
	case kind == KindReduce && in.CategoryID == nil:
		return invalidf("a reduction goal needs a category")
	case kind == KindReduce && (in.ReducePct == nil || *in.ReducePct < 1 || *in.ReducePct > 100):
		return invalidf("a reduction goal needs a percentage of 1–100")
	case kind == KindReduce && in.AccountID != nil:
		return invalidf("a reduction goal is over every account")
	}
	return nil
}

func pct16(p *int) *int16 {
	if p == nil {
		return nil
	}
	v := int16(*p) //nolint:gosec // 1..100, checked
	return &v
}

// owned checks that a referenced category or account is the user's (foreign keys don't see RLS).
func owned(ctx context.Context, q *db.Queries, userID uuid.UUID, in *Input) (currency string, err error) {
	if in.CategoryID != nil {
		if _, err := q.GetCategory(ctx, db.GetCategoryParams{UserID: userID, ID: *in.CategoryID}); err != nil {
			return "", fmt.Errorf("category %s: %w", *in.CategoryID, ledger.ErrNotFound)
		}
	}
	if in.AccountID != nil {
		a, err := q.GetAccount(ctx, db.GetAccountParams{UserID: userID, ID: *in.AccountID})
		if err != nil {
			return "", fmt.Errorf("account %s: %w", *in.AccountID, ledger.ErrNotFound)
		}
		return a.Currency, nil
	}
	return "", nil
}

// Create adds a goal; an assistant proposal records it as created (ADR 0030).
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Goal, error) {
	if err := s.clean(&in, true); err != nil {
		return Goal{}, err
	}
	if err := checkKind(in.Kind, &in, *in.StartsOn); err != nil {
		return Goal{}, err
	}
	var id uuid.UUID
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		if n, err := q.CountGoals(ctx, userID); err != nil || n >= MaxGoals {
			if err == nil {
				err = fmt.Errorf("at most %d goals: %w", MaxGoals, platform.ErrLimitReached)
			}
			return err
		}
		cur, err := owned(ctx, q, userID, &in)
		if err != nil {
			return err
		}
		if cur == "" {
			cur = in.Currency
		}
		if cur == "" {
			if accs, err := q.ListAccounts(ctx, userID); err == nil && len(accs) > 0 {
				cur = accs[0].Currency
			} else {
				cur = "PLN"
			}
		}
		g, err := q.InsertGoal(ctx, db.InsertGoalParams{ID: platform.NewID(), UserID: userID, Name: in.Name, Kind: in.Kind,
			TargetMinor: in.TargetMinor, Currency: cur, AccountID: in.AccountID, CategoryID: in.CategoryID, ReducePct: pct16(in.ReducePct),
			BaselineMonths: int16(in.BaselineMonths), StartsOn: *in.StartsOn, Deadline: in.Deadline, Now: s.clock.Now()}) //nolint:gosec // 1..12
		if err != nil {
			return err
		}
		id = g.ID
		st := StateOf(&g)
		st.Created = true
		return ledger.RecordAIChange(ctx, q, s.clock, userID, []ledger.AIItem{{Entity: ledger.AIEntityGoal, ID: g.ID, After: st}})
	})
	if err != nil {
		return Goal{}, ledger.DBError("create goal", err)
	}
	return s.Get(ctx, userID, id)
}

// Update changes a goal; a stale version is platform.ErrStaleVersion. An assistant proposal
// records what it replaced.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input) (Goal, error) {
	if err := s.clean(&in, false); err != nil {
		return Goal{}, err
	}
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetGoalForUpdate(ctx, db.GetGoalForUpdateParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if err := checkKind(cur.Kind, &in, cur.StartsOn); err != nil {
			return err
		}
		if _, err := owned(ctx, q, userID, &in); err != nil {
			return err
		}
		g, err := q.UpdateGoal(ctx, db.UpdateGoalParams{UserID: userID, ID: id, Version: int32(in.Version), Name: in.Name, //nolint:gosec // a version
			TargetMinor: in.TargetMinor, AccountID: in.AccountID, CategoryID: in.CategoryID, ReducePct: pct16(in.ReducePct),
			BaselineMonths: int16(in.BaselineMonths), Deadline: in.Deadline, Status: in.Status, Now: s.clock.Now()}) //nolint:gosec // 1..12
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("goal %s changed since version %d: %w", id, in.Version, platform.ErrStaleVersion)
		} else if err != nil {
			return err
		}
		return ledger.RecordAIChange(ctx, q, s.clock, userID, []ledger.AIItem{{Entity: ledger.AIEntityGoal, ID: id,
			Before: StateOf(&cur), After: StateOf(&g)}})
	})
	if err != nil {
		return Goal{}, ledger.DBError("update goal", err)
	}
	return s.Get(ctx, userID, id)
}

// Delete removes a goal.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	var n int64
	err := s.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		n, err = q.DeleteGoal(ctx, db.DeleteGoalParams{UserID: userID, ID: id})
		return err
	})
	if err == nil && n == 0 {
		err = pgx.ErrNoRows
	}
	return ledger.DBError("delete goal", err)
}

// CurrentState reads a goal's State inside the caller's transaction (an AI change's revert); nil
// when it is gone.
func CurrentState(ctx context.Context, q *db.Queries, userID, id uuid.UUID) (*State, error) {
	g, err := q.GetGoalForUpdate(ctx, db.GetGoalForUpdateParams{UserID: userID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	st := StateOf(&g)
	return &st, nil
}

// Same reports whether a goal still is as an AI change left it: every save bumps the version.
func (s State) Same(o State) bool { return s.Version == o.Version }

// RestoreTx puts a goal back as an AI change found it (an update's undo), or deletes a goal it
// created, inside the caller's transaction.
func RestoreTx(ctx context.Context, q *db.Queries, now time.Time, userID, id uuid.UUID, before *State) error {
	if before == nil {
		_, err := q.DeleteGoal(ctx, db.DeleteGoalParams{UserID: userID, ID: id})
		return err
	}
	cur, err := q.GetGoalForUpdate(ctx, db.GetGoalForUpdateParams{UserID: userID, ID: id})
	if err != nil {
		return err
	}
	_, err = q.UpdateGoal(ctx, db.UpdateGoalParams{UserID: userID, ID: id, Version: cur.Version, Name: before.Name,
		TargetMinor: before.TargetMinor, AccountID: before.AccountID, CategoryID: before.CategoryID, ReducePct: before.ReducePct,
		BaselineMonths: before.BaselineMonths, Deadline: before.Deadline, Status: before.Status, Now: now})
	return err
}

// Get returns one goal with its progress.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Goal, error) {
	list, err := s.list(ctx, userID, &id)
	if err != nil {
		return Goal{}, err
	}
	if len(list) == 0 {
		return Goal{}, fmt.Errorf("goal %s: %w", id, ledger.ErrNotFound)
	}
	return list[0], nil
}

// List returns every goal with its progress, open ones first, by deadline.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Goal, error) {
	return s.list(ctx, userID, nil)
}

func (s *Service) list(ctx context.Context, userID uuid.UUID, only *uuid.UUID) ([]Goal, error) {
	today := s.Today()
	out := []Goal{}
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListGoals(ctx, userID)
		if err != nil {
			return err
		}
		cats, err := q.ListCategories(ctx, userID)
		if err != nil {
			return err
		}
		catName := map[uuid.UUID]string{}
		for _, c := range cats {
			catName[c.ID] = c.Name
		}
		accs, err := q.ListAccounts(ctx, userID)
		if err != nil {
			return err
		}
		accName := map[uuid.UUID]string{}
		for i := range accs {
			accName[accs[i].ID] = accs[i].Name
		}
		aiIDs, err := q.AIGoalIDs(ctx, userID)
		if err != nil {
			return err
		}
		ai := map[uuid.UUID]bool{}
		for _, id := range aiIDs {
			ai[id] = true
		}
		for i := range rows {
			r := &rows[i]
			if only != nil && r.ID != *only {
				continue
			}
			g := Goal{ID: r.ID, Name: r.Name, Kind: r.Kind, TargetMinor: r.TargetMinor, Currency: r.Currency, AccountID: r.AccountID,
				CategoryID: r.CategoryID, BaselineMonths: int(r.BaselineMonths), StartsOn: r.StartsOn, Deadline: r.Deadline,
				Status: r.Status, Version: int(r.Version), AI: ai[r.ID]}
			if r.ReducePct != nil {
				v := int(*r.ReducePct)
				g.ReducePct = &v
			}
			if r.AccountID != nil {
				g.AccountName = accName[*r.AccountID]
			}
			if r.CategoryID != nil {
				g.CategoryName = catName[*r.CategoryID]
			}
			if g.Progress, err = progress(ctx, q, userID, r, today); err != nil {
				return err
			}
			out = append(out, g)
		}
		return nil
	})
	return out, err
}

// progress reads what a goal's rules need and applies them.
func progress(ctx context.Context, q *db.Queries, userID uuid.UUID, g *db.Goal, today platform.Date) (Progress, error) {
	to := today
	if g.Deadline.Before(today) {
		to = g.Deadline
	}
	switch g.Kind {
	case KindSave:
		var saved int64
		var err error
		if g.AccountID != nil {
			saved, err = q.GoalAccountGrowth(ctx, db.GoalAccountGrowthParams{UserID: userID, AccountID: *g.AccountID, FromOn: g.StartsOn, ToOn: to})
		} else {
			saved, err = q.GoalNet(ctx, db.GoalNetParams{UserID: userID, Currency: g.Currency, FromOn: g.StartsOn, ToOn: to})
		}
		if err != nil {
			return Progress{}, err
		}
		var target int64
		if g.TargetMinor != nil {
			target = *g.TargetMinor
		}
		return SaveProgress(g.StartsOn, g.Deadline, today, target, SaveFacts{SavedMinor: saved}), nil
	default:
		if g.CategoryID == nil || g.ReducePct == nil {
			return Progress{State: StateNeedsAttention}, nil
		}
		from, _ := BaselineRange(g.StartsOn, int(g.BaselineMonths))
		rows, err := q.GoalCategoryMonths(ctx, db.GoalCategoryMonthsParams{UserID: userID, CategoryID: g.CategoryID, FromOn: from, ToOn: today})
		if err != nil {
			return Progress{}, err
		}
		f := ReduceFacts{Months: map[platform.Date]int64{}}
		for _, r := range rows {
			f.Months[r.Month] = r.SpentMinor
		}
		return ReduceProgress(g.StartsOn, g.Deadline, today, int(*g.ReducePct), int(g.BaselineMonths), f), nil
	}
}
