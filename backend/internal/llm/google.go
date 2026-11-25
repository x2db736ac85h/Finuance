package llm

import (
	"fmt"
	"regexp"
	"strings"
)

// Google Cloud's hosts for Vertex AI (PLAN 26.2, ADR 0045). A connection that authenticates with
// Google credentials sends its bearer token to these and to nothing else: a wrong address in the
// configuration, or a redirect, can't carry the token to another host.
//
//	aiplatform.googleapis.com                the global endpoint
//	{location}-aiplatform.googleapis.com     a region (europe-west4-aiplatform.googleapis.com)
//	aiplatform.{multi-region}.rep.googleapis.com   a multi-region with a residency promise (eu, us)
var (
	googleRegional = regexp.MustCompile(`^[a-z]+(-[a-z]+)*[0-9]+-aiplatform\.googleapis\.com$`)
	googleMulti    = regexp.MustCompile(`^aiplatform\.[a-z]{2,12}\.rep\.googleapis\.com$`)
	googleLocation = regexp.MustCompile(`^(global|[a-z]{2,12}|[a-z]+(-[a-z]+)*[0-9]+)$`)
	googleProject  = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
)

// GoogleAPIHost reports whether host (without a port) is one of Vertex AI's.
func GoogleAPIHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "aiplatform.googleapis.com" || googleRegional.MatchString(host) || googleMulti.MatchString(host)
}

// GoogleLocation reports whether s can be a Vertex AI location: "global", a region
// ("europe-west4") or a multi-region ("eu").
func GoogleLocation(s string) bool { return googleLocation.MatchString(s) }

// GoogleProject reports whether s can be a Google Cloud project id.
func GoogleProject(s string) bool { return googleProject.MatchString(s) }

// GoogleBaseURL is the OpenAI-compatible endpoint of Vertex AI for a project and a location: the
// client appends /chat/completions. Callers check both with GoogleProject and GoogleLocation.
func GoogleBaseURL(project, location string) string {
	host := location + "-aiplatform.googleapis.com"
	switch {
	case location == "global":
		host = "aiplatform.googleapis.com"
	case !strings.ContainsAny(location, "-0123456789"): // a multi-region: eu, us
		host = "aiplatform." + location + ".rep.googleapis.com"
	}
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/endpoints/openapi", host, project, location)
}
