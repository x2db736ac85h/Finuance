package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Developer is the administrators' Developer section (FINUANCE_DEVELOPER_MODE, ADR 0025): the
// build, the source repository and links to the tools around the instance (dashboards, CI/CD),
// each with a note on how to get in. It is never shown to anyone but an administrator, and it holds
// no secrets: notes say how to get a credential (a command that needs cluster access), never the
// credential.
type Developer struct {
	Source *url.URL // the source repository; nil = not shown
	Links  []DeveloperLink
}

// DeveloperLink is one tool. URL is http(s) without credentials; Note and Command are plain text.
type DeveloperLink struct {
	Group    string // heading the link is listed under, e.g. "Observability"; empty = "Links"
	Name     string
	URL      *url.URL
	Note     string   // how to get in or where the credentials are
	Commands []string // commands to copy, e.g. the port-forward and reading the password
}

// Bounds of the links file: it is a short list written by the operator.
const (
	maxDeveloperFile  = 64 << 10
	maxDeveloperLinks = 40
	maxLinkName       = 60
	maxLinkGroup      = 40
	maxLinkURL        = 2000
	maxLinkText       = 600
	maxLinkCommands   = 4
)

// developerFile is the JSON layout of FINUANCE_DEVELOPER_LINKS_FILE (docs/admin.md).
type developerFile struct {
	Source string `json:"source"`
	Links  []struct {
		Group    string   `json:"group"`
		Name     string   `json:"name"`
		URL      string   `json:"url"`
		Note     string   `json:"note"`
		Commands []string `json:"commands"`
	} `json:"links"`
}

// developer reads the links file when developer mode is on. With the mode off the file isn't even
// opened, so switching the mode off is one variable.
func (p *parser) developer(on bool) *Developer {
	if !on {
		return nil
	}
	const fileVar = "FINUANCE_DEVELOPER_LINKS_FILE"
	d := &Developer{}
	file, ok := p.raw(fileVar)
	if !ok {
		return d // the build only
	}
	b, err := readLimitedFile(file, maxDeveloperFile, "developer links file")
	if err != nil {
		p.fail(fileVar, err.Error())
		return nil
	}
	var f developerFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		p.fail(fileVar, fmt.Sprintf("%s is not a valid developer links file: %v", file, err))
		return nil
	}
	var problems []string
	if f.Source != "" {
		u, msg := linkURL(f.Source)
		if msg != "" {
			problems = append(problems, "source: "+msg)
		}
		d.Source = u
	}
	if len(f.Links) > maxDeveloperLinks {
		problems = append(problems, fmt.Sprintf("at most %d links", maxDeveloperLinks))
	}
	for i := range f.Links {
		fl := &f.Links[i]
		where := fmt.Sprintf("link %d", i+1)
		if fl.Name != "" {
			where = fmt.Sprintf("link %q", fl.Name)
		}
		bad := func(msg string) { problems = append(problems, where+": "+msg) }
		name, group := strings.TrimSpace(fl.Name), strings.TrimSpace(fl.Group)
		if msg := plainText(name, maxLinkName, false); name == "" || msg != "" {
			bad(fmt.Sprintf("name must be 1–%d characters on one line", maxLinkName))
		}
		if msg := plainText(group, maxLinkGroup, false); msg != "" {
			bad("group " + msg)
		}
		if msg := plainText(fl.Note, maxLinkText, true); msg != "" {
			bad("note " + msg)
		}
		if len(fl.Commands) > maxLinkCommands {
			bad(fmt.Sprintf("at most %d commands", maxLinkCommands))
		}
		var commands []string
		for _, c := range fl.Commands {
			c = strings.TrimSpace(c)
			if msg := plainText(c, maxLinkText, false); c == "" || msg != "" {
				bad(fmt.Sprintf("each command must be 1–%d characters on one line", maxLinkText))
				break
			}
			commands = append(commands, c)
		}
		u, msg := linkURL(fl.URL)
		if msg != "" {
			bad(msg)
		}
		d.Links = append(d.Links, DeveloperLink{Group: group, Name: name, URL: u,
			Note: strings.TrimSpace(fl.Note), Commands: commands})
	}
	if len(problems) > 0 {
		p.fail(fileVar, file+": "+strings.Join(problems, "; "))
		return nil
	}
	return d
}

// linkURL accepts an absolute http(s) URL without user info: a password in a link would be shown
// to every administrator and kept in browser histories, and any other scheme (javascript:, data:)
// could run in the panel.
func linkURL(s string) (*url.URL, string) {
	u, err := url.Parse(s)
	switch {
	case len(s) > maxLinkURL:
		return nil, fmt.Sprintf("url is longer than %d characters", maxLinkURL)
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		return nil, "url must be an absolute http(s) URL" // not echoed: it may hold a password
	case u.User != nil:
		return nil, "url must not contain a user name or password (say where to find them in the note)"
	}
	return u, ""
}

// plainText checks a text field: valid UTF-8, at most limit characters, no control characters
// (line breaks only where multiline).
func plainText(s string, limit int, multiline bool) string {
	if !utf8.ValidString(s) {
		return "must be valid UTF-8"
	}
	if utf8.RuneCountInString(s) > limit {
		return fmt.Sprintf("must be at most %d characters", limit)
	}
	for _, r := range s {
		if r < 0x20 && (!multiline || r != '\n') || r == 0x7f {
			return "must not contain control characters"
		}
	}
	return ""
}
