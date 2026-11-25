// Command llmmock serves testutil/llmmock on an address, for the Compose ai-mock profile, the E2E
// tests and local UI work without a model (PLAN task 13.8). It is never part of the production
// image.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

func main() {
	addr := flag.String("addr", ":11434", "listen address")
	vertex := flag.String("vertex", "", "answer like Vertex AI for this project/location (e.g. demo-project/europe-west4), with the metadata server's token path")
	tokenTTL := flag.Duration("token-ttl", time.Hour, "how long a token of the Vertex mode is accepted")
	flag.Parse()
	mock := llmmock.New()
	if project, location, ok := strings.Cut(*vertex, "/"); ok {
		mock.Vertex = &llmmock.Vertex{Project: project, Location: location, TokenTTL: *tokenTTL}
		log.Printf("Vertex mode: %s, tokens for %s", mock.Vertex.Base(), *tokenTTL)
	}
	srv := &http.Server{Addr: *addr, Handler: mock, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("llmmock listening on %s (models: mock-tools, mock-text-tools, mock-no-tools)", *addr)
	log.Fatal(srv.ListenAndServe())
}
