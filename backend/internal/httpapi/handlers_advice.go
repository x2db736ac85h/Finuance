package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/advice"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// The advice library (ADR 0034): administrators write articles under /admin/advice; anyone signed
// in reads a published one, which the assistant's search_advice found.

func adviceDTO(a advice.Article) gen.AdviceArticle {
	return gen.AdviceArticle{Id: a.ID, Slug: a.Slug, Language: a.Language, Title: a.Title, Body: a.Body, BodyLength: a.BodyLength,
		Tags: a.Tags, Status: gen.AdviceArticleStatus(a.Status), Version: a.Version, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func (a *api) adviceService() (*advice.Service, error) {
	if a.advice == nil {
		return nil, platform.ErrNotFound
	}
	return a.advice, nil
}

// GetAdvice returns a published article for reading.
func (a *api) GetAdvice(ctx context.Context, req gen.GetAdviceRequestObject) (gen.GetAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	art, err := s.Published(ctx, identity(ctx).User.ID, req.Slug, deref(req.Params.Language))
	if err != nil {
		return nil, err
	}
	return gen.GetAdvice200JSONResponse(adviceDTO(art)), nil
}

// AdminListAdvice lists every article without bodies.
func (a *api) AdminListAdvice(ctx context.Context, _ gen.AdminListAdviceRequestObject) (gen.AdminListAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.AdminListAdvice200JSONResponse, 0, len(list))
	for i := range list {
		out = append(out, adviceDTO(list[i]))
	}
	return out, nil
}

// AdminGetAdvice returns one article with its body.
func (a *api) AdminGetAdvice(ctx context.Context, req gen.AdminGetAdviceRequestObject) (gen.AdminGetAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	art, err := s.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AdminGetAdvice200JSONResponse(adviceDTO(art)), nil
}

func adviceIn(slug, language, title, body string, tags []string, status string) advice.Input {
	return advice.Input{Slug: slug, Language: language, Title: title, Body: body, Tags: tags, Status: status}
}

func (a *api) auditAdvice(ctx context.Context, action string, art advice.Article) {
	a.adminAudit(ctx, action, nil, map[string]any{"slug": art.Slug, "language": art.Language, "status": art.Status})
}

// AdminCreateAdvice writes an article.
func (a *api) AdminCreateAdvice(ctx context.Context, req gen.AdminCreateAdviceRequestObject) (gen.AdminCreateAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	b := req.Body
	art, err := s.Create(ctx, adviceIn(b.Slug, b.Language, b.Title, b.Body, b.Tags, string(b.Status)))
	if err != nil {
		return nil, err
	}
	a.auditAdvice(ctx, ledger.AuditAdminAdviceChanged, art)
	return gen.AdminCreateAdvice201JSONResponse(adviceDTO(art)), nil
}

// AdminUpdateAdvice changes an article (versioned).
func (a *api) AdminUpdateAdvice(ctx context.Context, req gen.AdminUpdateAdviceRequestObject) (gen.AdminUpdateAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	b := req.Body
	art, err := s.Update(ctx, req.Id, b.Version, adviceIn(b.Slug, b.Language, b.Title, b.Body, b.Tags, string(b.Status)))
	if err != nil {
		return nil, err
	}
	a.auditAdvice(ctx, ledger.AuditAdminAdviceChanged, art)
	return gen.AdminUpdateAdvice200JSONResponse(adviceDTO(art)), nil
}

// AdminDeleteAdvice deletes an article.
func (a *api) AdminDeleteAdvice(ctx context.Context, req gen.AdminDeleteAdviceRequestObject) (gen.AdminDeleteAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	art, err := s.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	a.auditAdvice(ctx, ledger.AuditAdminAdviceDeleted, art)
	return gen.AdminDeleteAdvice204Response{}, nil
}

// AdminAddStarterAdvice adds the starter articles as drafts.
func (a *api) AdminAddStarterAdvice(ctx context.Context, _ gen.AdminAddStarterAdviceRequestObject) (gen.AdminAddStarterAdviceResponseObject, error) {
	s, err := a.adviceService()
	if err != nil {
		return nil, err
	}
	n, err := s.AddStarter(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		a.adminAudit(ctx, ledger.AuditAdminAdviceChanged, nil, map[string]any{"starter": n})
	}
	return gen.AdminAddStarterAdvice200JSONResponse{Added: n}, nil
}
