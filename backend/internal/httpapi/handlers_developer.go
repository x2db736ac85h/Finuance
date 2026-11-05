package httpapi

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

// AdminDeveloperInfo is the administrators' Developer section (FINUANCE_DEVELOPER_MODE, ADR 0025).
// The admin gate already answers 404 to everyone else; with the mode off administrators get the
// same 404, so neither learns that the section exists. Responses are never cached (headers
// middleware: Cache-Control no-store on every API response).
func (a *api) AdminDeveloperInfo(ctx context.Context, _ gen.AdminDeveloperInfoRequestObject) (gen.AdminDeveloperInfoResponseObject, error) {
	d := a.cfg.Developer
	if d == nil {
		return nil, fmt.Errorf("developer mode is off: %w", platform.ErrNotFound)
	}
	var schema int64
	if a.schemaVersion != nil {
		v, err := a.schemaVersion(ctx)
		if err != nil {
			return nil, err
		}
		schema = v
	}
	host, _ := os.Hostname()
	out := gen.AdminDeveloperInfo200JSONResponse{
		Build: gen.DeveloperBuild{Version: a.version, Commit: a.commit, Date: a.buildDate, GoVersion: runtime.Version(),
			Environment: string(a.cfg.Env), Instance: host, SchemaVersion: schema, ExpectedSchemaVersion: store.ExpectedVersion()},
		Links: make([]gen.DeveloperLink, 0, len(d.Links)),
	}
	if d.Source != nil {
		out.Source = sourceLinks(d.Source.String(), d.Source.Host, a.version, a.commit)
	}
	for _, l := range d.Links {
		group := l.Group
		if group == "" {
			group = "Links"
		}
		dl := gen.DeveloperLink{Group: group, Name: l.Name, Url: l.URL.String()}
		if l.Note != "" {
			dl.Note = &l.Note
		}
		if len(l.Commands) > 0 {
			dl.Commands = &l.Commands
		}
		out.Links = append(out.Links, dl)
	}
	return out, nil
}

var (
	commitSHA  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	releaseTag = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)
)

// sourceLinks adds the build's commit and release pages for a GitHub repository; the version and
// commit come from the build's ldflags, so they are checked before going into a URL.
func sourceLinks(repo, host, version, commit string) *gen.DeveloperSource {
	s := &gen.DeveloperSource{Url: repo}
	if host != "github.com" {
		return s
	}
	base := strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
	if commitSHA.MatchString(commit) {
		u := base + "/commit/" + commit
		s.CommitUrl = &u
	}
	if releaseTag.MatchString(version) {
		u := base + "/releases/tag/v" + version
		s.ReleaseUrl = &u
	}
	return s
}
