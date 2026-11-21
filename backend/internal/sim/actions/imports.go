package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

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
// waiting until the worker is done (two minutes at most, as the web app waits).
func Import(ctx context.Context, a *sim.Actor, account int, from, to platform.Date) error {
	pv, committed, err := upload(ctx, a, account, from, to)
	if err != nil {
		return err
	}
	if len(pv.Missing) > 0 {
		return fmt.Errorf("the statement's columns for %v were not found", pv.Missing)
	}
	id := pv.Import.Id
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
