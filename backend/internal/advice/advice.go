// Package advice is the advice library (Phase 20, ADR 0034): articles an administrator writes and
// publishes, which the assistant searches with search_advice and anyone signed in can read. An
// instance table outside RLS (like themes); search is PostgreSQL full-text search with the 'simple'
// configuration over text normalised here (lower case, accents folded), so every language works
// without an extension.
package advice

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Statuses.
const (
	Draft     = "draft"
	Published = "published"
)

// Limits (migration 00021).
const (
	MaxTitle   = 200
	MaxBody    = 20_000
	MaxTags    = 10
	MaxTag     = 40
	MaxResults = 3
	ExcerptLen = 1000
)

// Article is one article.
type Article struct {
	ID         uuid.UUID
	Slug       string
	Language   string
	Title      string
	Body       string // empty in a listing
	BodyLength int
	Tags       []string
	Status     string
	Version    int32
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Input is what an administrator writes.
type Input struct {
	Slug, Language, Title, Body, Status string
	Tags                                []string
}

// Hit is one search result as the assistant gets it.
type Hit struct {
	Slug     string `json:"slug"`
	Language string `json:"language"`
	Title    string `json:"title"`
	Excerpt  string `json:"excerpt"`
}

// Service reads and writes the library.
type Service struct {
	st    *store.Store
	clock platform.Clock
}

// New returns the service.
func New(st *store.Store, clock platform.Clock) *Service { return &Service{st: st, clock: clock} }

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	langRe = regexp.MustCompile(`^[a-z]{2}$`)
)

func (in *Input) clean() error {
	in.Slug, in.Language, in.Title = strings.TrimSpace(in.Slug), strings.ToLower(strings.TrimSpace(in.Language)), strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if in.Status == "" {
		in.Status = Draft
	}
	var tags []string
	for _, t := range in.Tags {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	in.Tags = tags
	if in.Tags == nil {
		in.Tags = []string{}
	}
	ve := &platform.ValidationError{}
	if !slugRe.MatchString(in.Slug) {
		ve.Add("slug", "must be lower-case letters, digits and dashes (at most 64)")
	}
	if !langRe.MatchString(in.Language) {
		ve.Add("language", "must be a two-letter language code such as en")
	}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > MaxTitle {
		ve.Add("title", fmt.Sprintf("must be 1 to %d characters", MaxTitle))
	}
	if utf8.RuneCountInString(in.Body) > MaxBody {
		ve.Add("body", fmt.Sprintf("must be at most %d characters", MaxBody))
	}
	if len(in.Tags) > MaxTags || slices.ContainsFunc(in.Tags, func(t string) bool { return utf8.RuneCountInString(t) > MaxTag }) {
		ve.Add("tags", fmt.Sprintf("at most %d tags of at most %d characters", MaxTags, MaxTag))
	}
	if in.Status != Draft && in.Status != Published {
		ve.Add("status", "must be draft or published")
	}
	for _, s := range []string{in.Title, in.Body} {
		if strings.ContainsRune(s, 0) {
			ve.Add("body", "contains a NUL character")
			break
		}
	}
	return ve.Err()
}

// normalised is the searchable form: lower case, accents folded, whitespace collapsed.
func normalised(s string) string { return strings.TrimSpace(ingest.Collapse(ingest.Norm(s))) }

func searchText(in Input) (title, body string) {
	return normalised(in.Title + " " + strings.Join(in.Tags, " ")), normalised(in.Body)
}

func articleOf(id uuid.UUID, slug, lang, title, body string, tags []string, status string, version int32, created, updated time.Time) Article {
	if tags == nil {
		tags = []string{}
	}
	return Article{ID: id, Slug: slug, Language: lang, Title: title, Body: body, BodyLength: utf8.RuneCountInString(body), Tags: tags,
		Status: status, Version: version, CreatedAt: created, UpdatedAt: updated}
}

func conflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		ve := &platform.ValidationError{}
		ve.Add("slug", "is taken by another article in this language")
		return ve
	}
	return err
}

// List returns every article without its body (administrators).
func (s *Service) List(ctx context.Context) ([]Article, error) {
	var out []Article
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListAdviceArticles(ctx)
		for i := range rows {
			r := &rows[i]
			a := articleOf(r.ID, r.Slug, r.Language, r.Title, "", r.Tags, r.Status, r.Version, r.CreatedAt, r.UpdatedAt)
			a.BodyLength = int(r.BodyLength)
			out = append(out, a)
		}
		return err
	})
	return out, err
}

// Get returns one article (administrators).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Article, error) {
	var out Article
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.GetAdviceArticle(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("article %s: %w", id, platform.ErrNotFound)
		}
		out = articleOf(r.ID, r.Slug, r.Language, r.Title, r.Body, r.Tags, r.Status, r.Version, r.CreatedAt, r.UpdatedAt)
		return err
	})
	return out, err
}

// Create adds an article.
func (s *Service) Create(ctx context.Context, in Input) (Article, error) {
	if err := in.clean(); err != nil {
		return Article{}, err
	}
	tn, bn := searchText(in)
	var out Article
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.InsertAdviceArticle(ctx, db.InsertAdviceArticleParams{ID: platform.NewID(), Slug: in.Slug, Language: in.Language,
			Title: in.Title, Body: in.Body, Tags: in.Tags, Status: in.Status, TitleN: tn, BodyN: bn, Now: s.clock.Now()})
		out = articleOf(r.ID, r.Slug, r.Language, r.Title, r.Body, r.Tags, r.Status, r.Version, r.CreatedAt, r.UpdatedAt)
		return err
	})
	return out, conflict(err)
}

// Update replaces an article if version is the stored one (else platform.ErrStaleVersion).
func (s *Service) Update(ctx context.Context, id uuid.UUID, version int32, in Input) (Article, error) {
	if err := in.clean(); err != nil {
		return Article{}, err
	}
	tn, bn := searchText(in)
	var out Article
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.UpdateAdviceArticle(ctx, db.UpdateAdviceArticleParams{ID: id, Version: version, Slug: in.Slug, Language: in.Language,
			Title: in.Title, Body: in.Body, Tags: in.Tags, Status: in.Status, TitleN: tn, BodyN: bn, Now: s.clock.Now()})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, gerr := q.GetAdviceArticle(ctx, id); errors.Is(gerr, pgx.ErrNoRows) {
				return fmt.Errorf("article %s: %w", id, platform.ErrNotFound)
			}
			return fmt.Errorf("article %s: %w", id, platform.ErrStaleVersion)
		}
		out = articleOf(r.ID, r.Slug, r.Language, r.Title, r.Body, r.Tags, r.Status, r.Version, r.CreatedAt, r.UpdatedAt)
		return err
	})
	return out, conflict(err)
}

// Delete removes an article.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteAdviceArticle(ctx, id)
		if err == nil && n == 0 {
			return fmt.Errorf("article %s: %w", id, platform.ErrNotFound)
		}
		return err
	})
}

// Published returns a published article by slug for a signed-in user: in the language asked for,
// else English, else any.
func (s *Service) Published(ctx context.Context, userID uuid.UUID, slug, language string) (Article, error) {
	var out Article
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.PublishedAdviceArticle(ctx, db.PublishedAdviceArticleParams{Slug: slug, Language: language})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("article %q: %w", slug, platform.ErrNotFound)
		}
		out = articleOf(r.ID, r.Slug, r.Language, r.Title, r.Body, r.Tags, r.Status, r.Version, r.CreatedAt, r.UpdatedAt)
		return err
	})
	return out, err
}

// stopWords are English words too common to search by (other languages' short words are left out
// by the length rule; their common long words only cost ranking, not matches).
var stopWords = map[string]bool{"the": true, "and": true, "for": true, "how": true, "what": true, "should": true, "can": true,
	"you": true, "your": true, "with": true, "from": true, "that": true, "this": true, "are": true, "was": true, "have": true,
	"does": true, "about": true, "into": true, "when": true, "which": true, "who": true, "why": true, "will": true, "would": true,
	"could": true, "there": true, "their": true, "they": true, "them": true, "then": true, "than": true, "some": true, "any": true,
	"much": true, "many": true, "more": true, "most": true, "get": true, "got": true, "make": true, "want": true, "need": true,
	"help": true, "best": true, "good": true, "way": true, "ways": true, "tips": true, "advice": true, "start": true,
	"not": true, "but": true, "all": true, "its": true, "our": true, "out": true, "too": true, "very": true, "just": true,
	"also": true, "been": true, "were": true, "has": true, "had": true, "did": true, "only": true, "each": true, "other": true,
	"here": true, "where": true, "while": true, "both": true, "now": true}

const maxTerms = 12

// Query turns a question into a tsquery for the 'simple' configuration: an OR of prefix terms made
// of the normalised words of 3 or more letters, stop words left out, at most 12. "" when nothing
// is left to search for.
func Query(text string) string {
	words := strings.FieldsFunc(normalised(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var terms []string
	for _, w := range words {
		if utf8.RuneCountInString(w) < 3 || stopWords[w] || slices.Contains(terms, w) {
			continue
		}
		terms = append(terms, w)
		if len(terms) == maxTerms {
			break
		}
	}
	for i, t := range terms {
		terms[i] = t + ":*"
	}
	return strings.Join(terms, " | ")
}

// Search finds published articles for a question, best first: in the language asked for, else in
// any language. It runs read only in the user's context, like every assistant tool.
func (s *Service) Search(ctx context.Context, userID uuid.UUID, text, language string, limit int) ([]Hit, error) {
	tsq := Query(text)
	if tsq == "" {
		return []Hit{}, nil
	}
	if limit <= 0 || limit > MaxResults {
		limit = MaxResults
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if !langRe.MatchString(language) {
		language = ""
	}
	out := []Hit{}
	err := s.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		for _, lang := range []string{language, ""} {
			rows, err := q.SearchAdvice(ctx, db.SearchAdviceParams{Query: tsq, Language: lang, MaxResults: int32(limit)}) //nolint:gosec // ≤ 3
			if err != nil {
				return err
			}
			for _, r := range rows {
				out = append(out, Hit{Slug: r.Slug, Language: r.Language, Title: r.Title, Excerpt: excerpt(r.Body)})
			}
			if len(out) > 0 || lang == "" {
				return nil
			}
		}
		return nil
	})
	return out, err
}

// excerpt is the start of an article, cut at a word boundary.
func excerpt(body string) string {
	if utf8.RuneCountInString(body) <= ExcerptLen {
		return body
	}
	r := []rune(body)[:ExcerptLen]
	cut := string(r)
	if i := strings.LastIndexAny(cut, " \n"); i > ExcerptLen/2 {
		cut = cut[:i]
	}
	return cut + " …"
}

//go:embed starter/*.md
var starterFS embed.FS

// Starter returns the starter library: eight original English articles (ADR 0034).
func Starter() ([]Input, error) {
	files, err := fs.Glob(starterFS, "starter/*.md")
	if err != nil {
		return nil, err
	}
	var out []Input
	for _, f := range files {
		raw, err := starterFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		in, err := parseStarter(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out = append(out, in)
	}
	return out, nil
}

// parseStarter reads a starter file: a front matter of slug, title and tags, then the markdown.
func parseStarter(raw string) (Input, error) {
	rest, ok := strings.CutPrefix(raw, "---\n")
	head, body, ok2 := strings.Cut(rest, "\n---\n")
	if !ok || !ok2 {
		return Input{}, errors.New("no front matter")
	}
	in := Input{Language: "en", Status: Draft, Body: strings.TrimSpace(body)}
	for _, line := range strings.Split(head, "\n") {
		k, v, _ := strings.Cut(line, ":")
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch strings.TrimSpace(k) {
		case "slug":
			in.Slug = v
		case "title":
			in.Title = v
		case "tags":
			for _, t := range strings.Split(v, ",") {
				in.Tags = append(in.Tags, strings.TrimSpace(t))
			}
		}
	}
	err := in.clean()
	return in, err
}

// AddStarter adds the starter articles whose slug isn't in the library in English yet, as drafts
// for an administrator to read and publish. It returns how many were added.
func (s *Service) AddStarter(ctx context.Context) (int, error) {
	starter, err := Starter()
	if err != nil {
		return 0, err
	}
	added := 0
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		have, err := q.AdviceSlugs(ctx)
		if err != nil {
			return err
		}
		for _, in := range starter {
			if slices.ContainsFunc(have, func(r db.AdviceSlugsRow) bool { return r.Language == in.Language && r.Slug == in.Slug }) {
				continue
			}
			tn, bn := searchText(in)
			if _, err := q.InsertAdviceArticle(ctx, db.InsertAdviceArticleParams{ID: platform.NewID(), Slug: in.Slug, Language: in.Language,
				Title: in.Title, Body: in.Body, Tags: in.Tags, Status: in.Status, TitleN: tn, BodyN: bn, Now: s.clock.Now()}); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}
