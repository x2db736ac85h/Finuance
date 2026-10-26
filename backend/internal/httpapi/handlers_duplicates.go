package httpapi

import (
	"context"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The duplicates window (PLAN task 10.13, §7.14).

func duplicateFieldsDTO(f *ledger.DuplicateFields) gen.DuplicateFields {
	return gen.DuplicateFields{BookedOn: dateDTO(f.BookedOn), ValueOn: dateDTO(f.ValueOn),
		Amount: moneyDTO(f.AmountMinor, f.Currency), BalanceAfter: moneyDTO(f.BalanceMinor, f.Currency),
		Payee: f.Payee, Description: f.Description, Reference: f.Reference, TxType: f.TxType,
		BankCategory: f.BankCategory, CounterpartyAccount: f.CounterpartyAccount}
}

func duplicateGroupDTO(g *ledger.DuplicateGroup) gen.DuplicateGroup {
	out := gen.DuplicateGroup{AccountId: g.AccountID, Fingerprint: hex.EncodeToString(g.Fingerprint),
		KeyFields: duplicateFieldsDTO(&g.Key), DifferingFields: make([]gen.DuplicateGroupDifferingFields, len(g.Differing)),
		Versions: make([]gen.DuplicateVersion, len(g.Versions))}
	for i, f := range g.Differing {
		out.DifferingFields[i] = gen.DuplicateGroupDifferingFields(f)
	}
	if g.Override != nil {
		out.Override = &gen.DuplicateDecision{Mode: gen.DuplicateDecisionMode(g.Override.Mode), ImportId: g.Override.ImportID}
	}
	for i := range g.Versions {
		v := &g.Versions[i]
		dv := gen.DuplicateVersion{ImportId: v.ImportID, Filename: v.Filename, CommittedAt: v.CommittedAt,
			Rows: make([]gen.DuplicateRow, len(v.Rows))}
		for k := range v.Rows {
			r := &v.Rows[k]
			dv.Rows[k] = gen.DuplicateRow{Occurrence: r.Occurrence, Fields: duplicateFieldsDTO(&r.Fields), Raw: rawDTO(r.Raw), InData: r.InData}
		}
		out.Versions[i] = dv
	}
	return out
}

// fingerprintIn decodes the path's hex fingerprint (the OpenAPI pattern already checked its form).
func fingerprintIn(s string) ([]byte, error) {
	fp, err := hex.DecodeString(s)
	if err != nil || len(fp) != 32 {
		ve := &platform.ValidationError{}
		ve.Add("fingerprint", "must be 64 hex digits")
		return nil, ve.Err()
	}
	return fp, nil
}

// ListDuplicates returns one page of duplicate groups.
func (a *api) ListDuplicates(ctx context.Context, req gen.ListDuplicatesRequestObject) (gen.ListDuplicatesResponseObject, error) {
	p := req.Params
	var ids []uuid.UUID
	if p.AccountIds != nil {
		ids = *p.AccountIds
	}
	limit := ledger.MaxDuplicatePage
	if p.Limit != nil {
		limit = *p.Limit
	}
	page, err := a.ledger.Duplicates(ctx, identity(ctx).User.ID, ids, deref(p.Q), deref(p.Offset), limit)
	if err != nil {
		return nil, err
	}
	out := gen.ListDuplicates200JSONResponse{Total: page.Total, Groups: make([]gen.DuplicateGroup, len(page.Groups))}
	for i := range page.Groups {
		out.Groups[i] = duplicateGroupDTO(&page.Groups[i])
	}
	return out, nil
}

// DecideDuplicate prefers one import's version or keeps both, then answers with the group.
func (a *api) DecideDuplicate(ctx context.Context, req gen.DecideDuplicateRequestObject) (gen.DecideDuplicateResponseObject, error) {
	userID := identity(ctx).User.ID
	fp, err := fingerprintIn(req.Fingerprint)
	if err != nil {
		return nil, err
	}
	// Only groups that exist can be decided (no overrides for keys the account doesn't have).
	if _, err := a.ledger.DuplicateGroupOf(ctx, userID, req.AccountId, fp); err != nil {
		return nil, err
	}
	if err := a.ledger.SetOverride(ctx, userID, req.AccountId, fp, ledger.OverrideMode(req.Body.Mode), req.Body.ImportId); err != nil {
		return nil, err
	}
	a.audit(ctx, userID, ledger.AuditDuplicateDecided, req.AccountId.String(),
		map[string]any{"fingerprint": req.Fingerprint, "mode": string(req.Body.Mode)})
	a.reclassifyAfter(ctx, userID) // the fingerprint's transactions were rebuilt
	g, err := a.ledger.DuplicateGroupOf(ctx, userID, req.AccountId, fp)
	if err != nil {
		return nil, err
	}
	return gen.DecideDuplicate200JSONResponse(duplicateGroupDTO(&g)), nil
}

// ResetDuplicate goes back to the default merge.
func (a *api) ResetDuplicate(ctx context.Context, req gen.ResetDuplicateRequestObject) (gen.ResetDuplicateResponseObject, error) {
	userID := identity(ctx).User.ID
	fp, err := fingerprintIn(req.Fingerprint)
	if err != nil {
		return nil, err
	}
	if err := a.ledger.ClearOverride(ctx, userID, req.AccountId, fp); err != nil {
		return nil, err
	}
	a.audit(ctx, userID, ledger.AuditDuplicateReset, req.AccountId.String(), map[string]any{"fingerprint": req.Fingerprint})
	a.reclassifyAfter(ctx, userID) // the fingerprint's transactions were rebuilt
	g, err := a.ledger.DuplicateGroupOf(ctx, userID, req.AccountId, fp)
	if err != nil {
		return nil, err
	}
	return gen.ResetDuplicate200JSONResponse(duplicateGroupDTO(&g)), nil
}
