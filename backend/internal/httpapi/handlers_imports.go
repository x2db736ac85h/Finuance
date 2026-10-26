package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Accounts (PLAN task 6.1).

// ListAccounts returns the accounts with transaction counts and date ranges.
func (a *api) ListAccounts(ctx context.Context, _ gen.ListAccountsRequestObject) (gen.ListAccountsResponseObject, error) {
	list, err := a.ledger.ListAccounts(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListAccounts200JSONResponse, len(list))
	for i := range list {
		out[i] = accountDTO(list[i])
	}
	return out, nil
}

// CreateAccount adds an account.
func (a *api) CreateAccount(ctx context.Context, req gen.CreateAccountRequestObject) (gen.CreateAccountResponseObject, error) {
	acc, err := a.ledger.CreateAccount(ctx, identity(ctx).User.ID, accountInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateAccount201JSONResponse(accountDTO(acc)), nil
}

// UpdateAccount replaces the editable fields of an account.
func (a *api) UpdateAccount(ctx context.Context, req gen.UpdateAccountRequestObject) (gen.UpdateAccountResponseObject, error) {
	u := identity(ctx).User.ID
	if _, err := a.ledger.UpdateAccount(ctx, u, req.Id, accountInput(*req.Body)); err != nil {
		return nil, err
	}
	acc, err := a.ledger.GetAccount(ctx, u, req.Id) // with the stats
	if err != nil {
		return nil, err
	}
	return gen.UpdateAccount200JSONResponse(accountDTO(acc)), nil
}

// DeleteAccount deletes an account with its imports and transactions.
func (a *api) DeleteAccount(ctx context.Context, req gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	u := identity(ctx).User.ID
	acc, err := a.ledger.GetAccount(ctx, u, req.Id) // its name, for the activity log
	if err != nil {
		return nil, err
	}
	if err := a.ledger.DeleteAccount(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditAccountDeleted, req.Id.String(), map[string]any{"name": acc.Name})
	return gen.DeleteAccount204Response{}, nil
}

// Imports (PLAN tasks 6.2–6.12).

// enqueueCommit returns the callback that inserts the CommitImport job in the ledger's transaction.
func (a *api) enqueueCommit(userID, importID uuid.UUID, remember bool) ledger.Enqueue {
	return func(ctx context.Context, tx pgx.Tx) error {
		if a.jobs == nil {
			return errors.New("job queue not configured")
		}
		_, err := a.jobs.InsertTx(ctx, tx, jobs.CommitImportArgs{ImportID: importID, UserID: userID, RememberMapping: remember}, nil)
		return err
	}
}

// UploadImport reads one file from the multipart body and returns its preview; with autoCommit a
// profile-matched, error-free file with a resolved account is committed right away (fast path).
func (a *api) UploadImport(ctx context.Context, req gen.UploadImportRequestObject) (gen.UploadImportResponseObject, error) {
	u := identity(ctx).User.ID
	if err := a.limits.upload.Allow(u.String()); err != nil {
		return nil, err
	}
	up, err := a.readUpload(req)
	if err != nil {
		return nil, err
	}
	p, err := a.ledger.CreatePreview(ctx, u, up)
	var dup ledger.ErrDuplicateFile
	if errors.As(err, &dup) {
		pr := ProblemFor(err, nil, a.prod)
		return gen.UploadImport409ApplicationProblemPlusJSONResponse{Type: pr.Type, Title: pr.Title, Status: pr.Status, Code: pr.Code,
			Detail: pr.Detail, RequestId: requestID(ctx), Instance: ptr("/api/v1/imports"), ExistingImportId: dup.ExistingID}, nil
	}
	if err != nil {
		return nil, err
	}
	labels, err := a.ledger.LayoutLabels(ctx, u)
	if err != nil {
		return nil, err
	}
	auto := req.Params.AutoCommit != nil && *req.Params.AutoCommit
	if auto && p.Import.MappingSource == "profile" && p.Stats.Errors == 0 && p.Import.AccountID != nil && len(p.Missing) == 0 {
		imp, err := a.ledger.StartCommit(ctx, u, p.Import.ID, a.enqueueCommit(u, p.Import.ID, false))
		if err != nil {
			return nil, err
		}
		p.Import = imp
		return gen.UploadImport202JSONResponse(previewDTO(p, labels)), nil
	}
	return gen.UploadImport201JSONResponse(previewDTO(p, labels)), nil
}

// readUpload takes the "file" part (exactly one) and the optional "accountId" field. The body is
// already capped by the MaxBytes middleware; the file itself is checked against the exact limit.
func (a *api) readUpload(req gen.UploadImportRequestObject) (ledger.Upload, error) {
	var up ledger.Upload
	files := 0
	ve := &platform.ValidationError{}
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return up, err
		}
		switch part.FormName() {
		case "file":
			files++
			if files > 1 {
				ve.Add("file", "send one file per request")
				break
			}
			up.Filename = cleanFilename(part.FileName())
			up.Content, err = io.ReadAll(io.LimitReader(part, a.cfg.MaxUploadBytes+1))
			if err != nil {
				return up, err
			}
			if int64(len(up.Content)) > a.cfg.MaxUploadBytes {
				return up, fmt.Errorf("the file is larger than %d bytes: %w", a.cfg.MaxUploadBytes, platform.ErrPayloadTooLarge)
			}
		case "accountId":
			b, err := io.ReadAll(io.LimitReader(part, 64))
			if err != nil {
				return up, err
			}
			id, err := uuid.Parse(strings.TrimSpace(string(b)))
			if err != nil {
				ve.Add("accountId", "must be a UUID")
				break
			}
			up.AccountID = &id
		}
	}
	if files == 0 {
		ve.Add("file", "is required")
	} else if len(up.Content) == 0 {
		ve.Add("file", "is empty")
	}
	return up, ve.Err()
}

// cleanFilename keeps the base name of an uploaded file, at most 255 characters.
func cleanFilename(name string) string {
	name = strings.TrimSpace(name[strings.LastIndexAny(name, `/\`)+1:])
	if name == "" {
		name = "upload.csv"
	}
	if r := []rune(name); len(r) > 255 {
		name = string(r[:255])
	}
	return name
}

// ListImports lists the user's imports, newest first, optionally for one account.
func (a *api) ListImports(ctx context.Context, req gen.ListImportsRequestObject) (gen.ListImportsResponseObject, error) {
	u := identity(ctx).User.ID
	list, err := a.ledger.ListImports(ctx, u, 1000)
	if err != nil {
		return nil, err
	}
	labels, err := a.ledger.LayoutLabels(ctx, u)
	if err != nil {
		return nil, err
	}
	out := gen.ListImports200JSONResponse{}
	for i := range list {
		if req.Params.AccountId != nil && (list[i].AccountID == nil || *list[i].AccountID != *req.Params.AccountId) {
			continue
		}
		out = append(out, importSummaryDTO(list[i], labels))
	}
	return out, nil
}

// GetImport returns one import (the commit's progress is polled here).
func (a *api) GetImport(ctx context.Context, req gen.GetImportRequestObject) (gen.GetImportResponseObject, error) {
	u := identity(ctx).User.ID
	imp, err := a.ledger.GetImport(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	labels, err := a.ledger.LayoutLabels(ctx, u)
	if err != nil {
		return nil, err
	}
	return gen.GetImport200JSONResponse(importDetailDTO(imp, labels)), nil
}

// UpdateImport applies account, mapping and option changes to a previewed import.
func (a *api) UpdateImport(ctx context.Context, req gen.UpdateImportRequestObject) (gen.UpdateImportResponseObject, error) {
	u := identity(ctx).User.ID
	p, err := a.ledger.UpdatePreview(ctx, u, req.Id, ledger.Changes{AccountID: req.Body.AccountId, Mapping: req.Body.Mapping, Options: req.Body.Options})
	if err != nil {
		return nil, err
	}
	labels, err := a.ledger.LayoutLabels(ctx, u)
	if err != nil {
		return nil, err
	}
	return gen.UpdateImport200JSONResponse(previewDTO(p, labels)), nil
}

// DeleteImport cancels a preview or removes a committed import (orphan-safe).
func (a *api) DeleteImport(ctx context.Context, req gen.DeleteImportRequestObject) (gen.DeleteImportResponseObject, error) {
	u := identity(ctx).User.ID
	imp, err := a.ledger.GetImport(ctx, u, req.Id) // its file name and state, for the activity log
	if err != nil {
		return nil, err
	}
	removed, err := a.ledger.DeleteImport(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditImportDeleted, req.Id.String(),
		map[string]any{"file": imp.Filename, "status": imp.Status, "transactionsRemoved": removed})
	a.reclassifyAfter(ctx, u) // re-materialised rows and broken transfer pairs
	return gen.DeleteImport204Response{}, nil
}

// ListImportRows pages through the staged rows.
func (a *api) ListImportRows(ctx context.Context, req gen.ListImportRowsRequestObject) (gen.ListImportRowsResponseObject, error) {
	u := identity(ctx).User.ID
	offset, limit, onlyErrors := 0, 100, false
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.OnlyErrors != nil {
		onlyErrors = *req.Params.OnlyErrors
	}
	rows, err := a.ledger.ImportRowsPage(ctx, u, req.Id, onlyErrors, offset, limit)
	if err != nil {
		return nil, err
	}
	counts, err := a.ledger.ImportRowCounts(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	total := counts.Rows
	if onlyErrors {
		total = counts.Errors
	}
	return gen.ListImportRows200JSONResponse{Rows: rowsDTO(rows), Total: total}, nil
}

// ListImportLines returns the first decoded records with their status.
func (a *api) ListImportLines(ctx context.Context, req gen.ListImportLinesRequestObject) (gen.ListImportLinesResponseObject, error) {
	limit := ledger.LinesLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	l, err := a.ledger.ImportLines(ctx, identity(ctx).User.ID, req.Id, limit)
	if err != nil {
		return nil, err
	}
	return gen.ListImportLines200JSONResponse(linesDTO(l)), nil
}

// GetImportFile returns the original bytes as an attachment.
func (a *api) GetImportFile(ctx context.Context, req gen.GetImportFileRequestObject) (gen.GetImportFileResponseObject, error) {
	imp, content, err := a.ledger.ImportFile(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": imp.Filename})
	return gen.GetImportFile200ApplicationoctetStreamResponse{Body: bytes.NewReader(content), ContentLength: int64(len(content)),
		Headers: gen.GetImportFile200ResponseHeaders{ContentDisposition: &cd}}, nil
}

// CommitImport starts the commit job of a previewed import.
func (a *api) CommitImport(ctx context.Context, req gen.CommitImportRequestObject) (gen.CommitImportResponseObject, error) {
	u := identity(ctx).User.ID
	remember := true
	if req.Body != nil && req.Body.RememberMapping != nil {
		remember = *req.Body.RememberMapping
	}
	imp, err := a.ledger.StartCommit(ctx, u, req.Id, a.enqueueCommit(u, req.Id, remember))
	if err != nil {
		return nil, err
	}
	return gen.CommitImport202JSONResponse(importDetailDTO(imp, nil)), nil
}

// ReprocessImport re-reads a committed import with a new mapping or options and commits it again.
func (a *api) ReprocessImport(ctx context.Context, req gen.ReprocessImportRequestObject) (gen.ReprocessImportResponseObject, error) {
	u := identity(ctx).User.ID
	imp, err := a.ledger.Reprocess(ctx, u, req.Id, ledger.Changes{Mapping: req.Body.Mapping, Options: req.Body.Options},
		a.enqueueCommit(u, req.Id, false))
	if err != nil {
		return nil, err
	}
	return gen.ReprocessImport202JSONResponse(importDetailDTO(imp, nil)), nil
}
