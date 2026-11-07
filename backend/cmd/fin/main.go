// Command fin is the Finuance API from the command line (Phase 20, 20.13; internal/fincli).
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/x2db736ac85h/finuance/backend/internal/fincli"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := fincli.Run(ctx, os.Args[1:], fincli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv,
		ConfigDir: os.UserConfigDir, Version: version})
	stop()
	os.Exit(code)
}
