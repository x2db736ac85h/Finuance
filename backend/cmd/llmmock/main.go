// Command llmmock serves testutil/llmmock on an address, for the Compose ai-mock profile, the E2E
// tests and local UI work without a model (PLAN task 13.8). It is never part of the production
// image.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

func main() {
	addr := flag.String("addr", ":11434", "listen address")
	flag.Parse()
	srv := &http.Server{Addr: *addr, Handler: llmmock.New(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("llmmock listening on %s (models: mock-tools, mock-text-tools, mock-no-tools)", *addr)
	log.Fatal(srv.ListenAndServe())
}
