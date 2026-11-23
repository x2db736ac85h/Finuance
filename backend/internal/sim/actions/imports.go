package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

// Imports: a new statement each month (by the persona's data calendar), a statement that overlaps
// the last one, a changed mapping before the commit, an abandoned preview, a statement deleted and
// imported again, and a broken file (a deliberate mistake).
func imports() []sim.Action {
	return []sim.Action{
		{Name: "imports.month", Area: "imports", Cost: sim.Heavy, Needs: sim.NeedAccounts, Run: importMonth},
		{Name: "imports.overlap", Area: "imports", Cost: sim.Heavy, Needs: sim.NeedAccounts | sim.NeedData, Run: importOverlap},
		{Name: "imports.remap", Area: "imports", Cost: sim.Heavy, Needs: sim.NeedAccounts | sim.NeedData, Run: importRemap},
		{Name: "imports.abandon", Area: "imports", Needs: sim.NeedAccounts | sim.NeedData, Run: importAbandon},
		{Name: "imports.again", Area: "imports", Cost: sim.Heavy, Needs: sim.NeedAccounts | sim.NeedData, Run: importAgain},
		{Name: "imports.broken", Area: "imports", Mistake: true, Run: importBroken},
	}
}

// maxSpan is the most days one statement covers (a first year of history comes in parts).
const maxSpan = 92

// importMonth imports what the persona's calendar has reached since its last import, at least a
// week of it (a person exports when a month is over, or after a few weeks). When the calendar
// runs fast and has reached today, the persona deletes its two accounts and starts again three
// years back, which exercises the deletion cascade.
func importMonth(ctx context.Context, a *sim.Actor) error {
	account := sim.Pick(a, statements.Current, statements.Current, statements.Current, statements.Savings)
	to := sim.AddDays(a.DataDay(), -1)
	from := sim.AddDays(a.ImportedThrough(account), 1)
	if to.Compare(from) < 0 || daysBetween(from, to) < 6 {
		if cfg, _ := a.Engine().Config(); cfg.ClockRate > 1 && a.DataDay() == a.Today() {
			return restart(ctx, a)
		}
		return sim.ErrNothingToDo
	}
	if daysBetween(from, to) >= maxSpan {
		to = sim.AddDays(from, maxSpan-1)
	}
	if err := Import(ctx, a, account, from, to); err != nil {
		return err
	}
	a.SetImportedThrough(account, to)
	return nil
}

// importOverlap imports a statement that starts three weeks before the last import ended (a
// person who isn't sure what they imported last time); the overlap de-duplicates.
func importOverlap(ctx context.Context, a *sim.Actor) error {
	account := sim.Pick(a, statements.Current, statements.Savings)
	last := a.ImportedThrough(account)
	to := sim.AddDays(a.DataDay(), -1)
	if to.Compare(last) < 0 {
		to = last
	}
	return Import(ctx, a, account, sim.AddDays(last, -21), to)
}

// importRemap uploads a statement, sends the mapping back (as a person who opened the column
// menu and changed their mind does: the server reads the file again) and commits.
func importRemap(ctx context.Context, a *sim.Actor) error {
	last := a.ImportedThrough(statements.Current)
	pv, committed, err := upload(ctx, a, statements.Current, sim.AddDays(last, -30), last)
	if err != nil || committed {
		return err
	}
	id := pv.Import.Id
	r, err := a.API.UpdateImportWithResponse(ctx, id, gen.UpdateImportJSONRequestBody{Mapping: &pv.Detected})
	if err != nil {
		return err
	}
	if err := sim.Expect("change the mapping", r.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	c, err := a.API.CommitImportWithResponse(ctx, id, gen.CommitImportJSONRequestBody{})
	if err != nil {
		return err
	}
	if err := sim.Expect("commit", c.HTTPResponse, http.StatusAccepted); err != nil {
		return err
	}
	return waitImport(ctx, a, id)
}

// importAbandon uploads a statement and leaves the preview without committing it, then deletes
// the persona's older abandoned previews (so they don't pile up over days).
func importAbandon(ctx context.Context, a *sim.Actor) error {
	last := a.ImportedThrough(statements.Current)
	pv, committed, err := upload(ctx, a, statements.Current, sim.AddDays(last, -10), last)
	if err != nil || committed {
		return err
	}
	list, err := a.API.ListImportsWithResponse(ctx, nil)
	if err != nil {
		return err
	}
	if err := sim.Expect("imports", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	for i := range *list.JSON200 {
		imp := &(*list.JSON200)[i]
		if imp.Status != gen.ImportSummaryStatusPreviewed || imp.Id == pv.Import.Id {
			continue
		}
		d, err := a.API.DeleteImportWithResponse(ctx, imp.Id)
		if err != nil {
			return err
		}
		if err := sim.Expect("delete a preview", d.HTTPResponse, http.StatusNoContent, http.StatusNotFound); err != nil {
			return err
		}
	}
	return nil
}

// importAgain deletes the persona's newest committed import and imports the same days again.
func importAgain(ctx context.Context, a *sim.Actor) error {
	list, err := a.API.ListImportsWithResponse(ctx, nil)
	if err != nil {
		return err
	}
	if err := sim.Expect("imports", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	for i := range *list.JSON200 {
		imp := &(*list.JSON200)[i]
		from, to, ok := rangeOf(imp.Filename)
		if imp.Status != gen.ImportSummaryStatusCommitted || imp.AccountId == nil || !ok {
			continue
		}
		account := statements.Current
		if *imp.AccountId == a.Account(statements.Savings) {
			account = statements.Savings
		} else if *imp.AccountId != a.Account(statements.Current) {
			continue // not one of the persona's bank accounts
		}
		d, err := a.API.DeleteImportWithResponse(ctx, imp.Id)
		if err != nil {
			return err
		}
		if err := sim.Expect("delete an import", d.HTTPResponse, http.StatusNoContent); err != nil {
			return err
		}
		return Import(ctx, a, account, from, to)
	}
	return sim.ErrNothingToDo
}

// broken are files a person drops by mistake: a picture, an empty file, a table without dates.
var broken = []struct {
	name    string
	content []byte
	status  []int
}{
	{"zdjecie.csv", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), []int{http.StatusUnsupportedMediaType}},
	{"pusty.csv", []byte(""), []int{http.StatusUnprocessableEntity, http.StatusBadRequest}},
	{"notatki.csv", []byte("lista zakupów;mleko;chleb\nna jutro;jajka;masło\n"), []int{http.StatusCreated, http.StatusUnprocessableEntity}},
}

// importBroken uploads a file that is not a statement; the answer refuses it or shows a preview
// with nothing to import, which the persona leaves (and deletes).
func importBroken(ctx context.Context, a *sim.Actor) error {
	b := sim.Pick(a, broken...)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", b.name)
	if err != nil {
		return err
	}
	_, _ = part.Write(b.content)
	if err := mw.Close(); err != nil {
		return err
	}
	r, err := a.API.UploadImportWithBodyWithResponse(ctx, &gen.UploadImportParams{}, mw.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	if err := sim.Expect("a broken file", r.HTTPResponse, b.status...); err != nil {
		return err
	}
	if r.JSON201 != nil {
		d, err := a.API.DeleteImportWithResponse(ctx, r.JSON201.Import.Id)
		if err != nil {
			return err
		}
		return sim.Expect("delete the preview", d.HTTPResponse, http.StatusNoContent)
	}
	return nil
}

// restart deletes the persona's two bank accounts, makes them again and starts its calendar
// three years back.
func restart(ctx context.Context, a *sim.Actor) error {
	for _, n := range []int{statements.Current, statements.Savings} {
		d, err := a.API.DeleteAccountWithResponse(ctx, a.Account(n))
		if err != nil {
			return err
		}
		if err := sim.Expect("delete an account", d.HTTPResponse, http.StatusNoContent); err != nil {
			return err
		}
		made, err := a.API.CreateAccountWithResponse(ctx, gen.CreateAccountJSONRequestBody{Name: statements.AccountName(n)})
		if err != nil {
			return err
		}
		if err := sim.Expect("make an account", made.HTTPResponse, http.StatusCreated); err != nil {
			return err
		}
		a.SetAccount(n, made.JSON201.Id)
	}
	a.Restart()
	return nil
}

// rangeOf reads the days a statement covers from its file name (statements.File names it).
func rangeOf(name string) (platform.Date, platform.Date, bool) {
	m := fileRange.FindStringSubmatch(name)
	if m == nil {
		return platform.Date{}, platform.Date{}, false
	}
	from, err1 := platform.ParseDate(m[1])
	to, err2 := platform.ParseDate(m[2])
	return from, to, err1 == nil && err2 == nil
}

var fileRange = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})-(\d{4}-\d{2}-\d{2})\.csv$`)

func daysBetween(from, to platform.Date) int {
	return int(to.Time().Sub(from.Time()) / (24 * time.Hour))
}

// upload sends one of the persona's statements to the upload route, as the import window does,
// and returns the preview (201) or the import the fast path already committed (202).
func upload(ctx context.Context, a *sim.Actor, account int, from, to platform.Date) (*gen.ImportPreview, bool, error) {
	stamp := fmt.Sprintf("sim-%s-%d", a.Persona.Slug, a.Now().UnixNano())
	name, content := statements.File(a.Persona, account, from, to, stamp)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("accountId", a.Account(account).String())
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return nil, false, err
	}
	_, _ = part.Write(content)
	if err := mw.Close(); err != nil {
		return nil, false, err
	}
	r, err := a.API.UploadImportWithBodyWithResponse(ctx, &gen.UploadImportParams{}, mw.FormDataContentType(), &body)
	if err != nil {
		return nil, false, err
	}
	if err := sim.Expect("upload", r.HTTPResponse, http.StatusCreated, http.StatusAccepted); err != nil {
		return nil, false, err
	}
	if r.JSON202 != nil {
		return r.JSON202, true, nil
	}
	return r.JSON201, false, nil
}

// Import uploads a statement of the persona's account for the days from … to and commits it,
// waiting until the worker is done (two minutes at most, as the web app waits). A statement
// without a row (a week of a savings account with no transfer and no interest) can't be committed:
// its preview is deleted, as a person closes the window, and the days count as imported.
func Import(ctx context.Context, a *sim.Actor, account int, from, to platform.Date) error {
	pv, committed, err := upload(ctx, a, account, from, to)
	if err != nil {
		return err
	}
	id := pv.Import.Id
	if !committed && pv.Stats.Rows-pv.Stats.Errors == 0 { // also before the columns: without rows none can be read
		d, err := a.API.DeleteImportWithResponse(ctx, id)
		if err != nil {
			return err
		}
		return sim.Expect("delete the empty preview", d.HTTPResponse, http.StatusNoContent)
	}
	if len(pv.Missing) > 0 {
		return fmt.Errorf("the statement's columns for %v were not found", pv.Missing)
	}
	if !committed {
		r, err := a.API.CommitImportWithResponse(ctx, id, gen.CommitImportJSONRequestBody{})
		if err != nil {
			return err
		}
		if err := sim.Expect("commit", r.HTTPResponse, http.StatusAccepted); err != nil {
			return err
		}
	}
	if err := waitImport(ctx, a, id); err != nil {
		return err
	}
	a.SetHasData(true)
	return nil
}

// errImportFailed: the worker marked the import failed.
var errImportFailed = errors.New("the import failed")

func waitImport(ctx context.Context, a *sim.Actor, id uuid.UUID) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		r, err := a.API.GetImportWithResponse(ctx, id)
		if err != nil {
			return err
		}
		if err := sim.Expect("import status", r.HTTPResponse, http.StatusOK); err != nil {
			return err
		}
		switch r.JSON200.Status {
		case gen.ImportDetailStatusCommitted:
			return nil
		case gen.ImportDetailStatusFailed:
			return fmt.Errorf("%w: %s", errImportFailed, deref(r.JSON200.Error))
		}
		t := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return fmt.Errorf("import %s: still not committed after two minutes", id)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
