package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Legacy migration (PLAN §4.10, task 14.1): the browser sends what it read from the backup, the
// worker runs it, the wizard polls the run.

const (
	maxManifestBytes = 16 << 20
	// legacyFilesFactor: the files of one migration may add up to this many upload limits.
	legacyFilesFactor = 4
)

// legacyBodyLimit is the largest body of a migration: four files and the manifest, and never more
// than FINUANCE_MAX_LEGACY_IMPORT_BYTES when that is set (it was read and never applied; IMP-3).
func legacyBodyLimit(maxUpload, maxLegacy int64) int64 {
	limit := legacyFilesFactor*maxUpload + maxManifestBytes + multipartOverhead
	if maxLegacy > 0 {
		limit = min(limit, maxLegacy)
	}
	return limit
}

// beforeBody runs after the session is known and before the request validator, which reads the
// whole body to check it. A migration's body can be close to a hundred megabytes, so what can
// refuse it is asked first: the upload limit per user, and that no run of the user is queued or
// running (IMP-3). Create refuses a second run too, but only once everything was read and decoded.
func (a *api) beforeBody(prod bool) gen.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Pattern == "POST /api/v1/legacy-imports" && a.legacy != nil && !a.cfg.DemoMode {
				ctx := r.Context()
				u := identity(ctx).User.ID
				err := a.limits.upload.Allow(u.String())
				if err == nil {
					err = a.legacy.Busy(ctx, u)
				}
				if err != nil {
					WriteProblem(w, r, err, prod)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CreateLegacyImport stores the manifest and files and enqueues the run.
func (a *api) CreateLegacyImport(ctx context.Context, req gen.CreateLegacyImportRequestObject) (gen.CreateLegacyImportResponseObject, error) {
	if a.cfg.DemoMode { // demo users are ephemeral, and the endpoint takes large uploads (PLAN 19.1)
		return nil, fmt.Errorf("the migration from the single-file app isn't available in the demo: %w", platform.ErrForbidden)
	}
	u := identity(ctx).User.ID
	man, files, err := a.readLegacyUpload(req) // beforeBody has checked the limit and that no run is active
	if err != nil {
		return nil, err
	}
	run, err := a.legacy.Create(ctx, u, man, files, func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		if a.jobs == nil {
			return errors.New("job queue not configured")
		}
		_, err := a.jobs.InsertTx(ctx, tx, jobs.LegacyImportArgs{UserID: u, ID: id}, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditLegacyImport, run.ID.String(), map[string]any{"files": len(files)})
	return gen.CreateLegacyImport202JSONResponse(legacyDTO(run)), nil
}

// readLegacyUpload reads the "manifest" part and the "file" parts, in order.
func (a *api) readLegacyUpload(req gen.CreateLegacyImportRequestObject) (legacyimport.Manifest, [][]byte, error) {
	var man legacyimport.Manifest
	var files [][]byte
	var total int64
	seenManifest := false
	ve := &platform.ValidationError{}
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return man, nil, err
		}
		switch part.FormName() {
		case "manifest":
			b, err := io.ReadAll(io.LimitReader(part, maxManifestBytes+1))
			if err != nil {
				return man, nil, err
			}
			if len(b) > maxManifestBytes {
				return man, nil, fmt.Errorf("the manifest is larger than %d bytes: %w", maxManifestBytes, platform.ErrPayloadTooLarge)
			}
			dec := json.NewDecoder(bytes.NewReader(b))
			if err := dec.Decode(&man); err != nil {
				ve.Add("manifest", "is not a valid migration manifest: "+err.Error())
			}
			seenManifest = true
		case "file":
			if len(files) == legacyimport.MaxFiles {
				ve.Add("file", fmt.Sprintf("at most %d files", legacyimport.MaxFiles))
				continue
			}
			b, err := io.ReadAll(io.LimitReader(part, a.cfg.MaxUploadBytes+1))
			if err != nil {
				return man, nil, err
			}
			if int64(len(b)) > a.cfg.MaxUploadBytes {
				return man, nil, fmt.Errorf("a file is larger than %d bytes: %w", a.cfg.MaxUploadBytes, platform.ErrPayloadTooLarge)
			}
			if total += int64(len(b)); total > legacyFilesFactor*a.cfg.MaxUploadBytes {
				return man, nil, fmt.Errorf("the files add up to more than %d bytes: %w", legacyFilesFactor*a.cfg.MaxUploadBytes, platform.ErrPayloadTooLarge)
			}
			files = append(files, b)
		}
	}
	if !seenManifest {
		ve.Add("manifest", "is required")
	}
	return man, files, ve.Err()
}

// ListLegacyImports returns the latest runs.
func (a *api) ListLegacyImports(ctx context.Context, _ gen.ListLegacyImportsRequestObject) (gen.ListLegacyImportsResponseObject, error) {
	u := identity(ctx).User.ID
	runs, err := a.legacy.List(ctx, u)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListLegacyImports200JSONResponse, 0, len(runs))
	for i := range runs {
		out = append(out, legacyDTO(runs[i]))
	}
	return out, nil
}

// GetLegacyImport returns one run.
func (a *api) GetLegacyImport(ctx context.Context, req gen.GetLegacyImportRequestObject) (gen.GetLegacyImportResponseObject, error) {
	u := identity(ctx).User.ID
	r, err := a.legacy.Get(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetLegacyImport200JSONResponse(legacyDTO(r)), nil
}

func legacyDTO(r legacyimport.Run) gen.LegacyImport {
	out := gen.LegacyImport{Id: r.ID, Status: gen.LegacyImportStatus(r.Status), Progress: r.Progress, Report: r.Report,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
	if r.Error != "" {
		out.Error = &r.Error
	}
	return out
}
