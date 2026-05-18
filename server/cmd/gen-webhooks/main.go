// Command gen-webhooks generates both catalog artifacts for the outbox events
// package:
//
//   - catalog_gen.go — the Go registry (All slice), generated from AST
//   - catalog_gen.yaml   — the OpenAPI 3.1 webhook spec, generated from runtime values
//
// If catalog_gen.go is ever accidentally deleted, restore it with a minimal
// stub (see its header) and then re-run this tool.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/server/internal/outbox/cataloggen"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// eventsDir is relative to the server/ directory where the mise task runs.
const eventsDir = "./internal/outbox/events"

func main() {
	check := flag.Bool("check", false, "verify both generated files are up to date without writing")
	flag.Parse()

	var errs []error
	if *check {
		if err := cataloggen.Check(eventsDir); err != nil {
			errs = append(errs, err)
		}
		if err := cataloggen.CheckYAML(eventsDir, events.All); err != nil {
			errs = append(errs, err)
		}
	} else {
		if err := cataloggen.Write(eventsDir); err != nil {
			errs = append(errs, err)
		}
		if err := cataloggen.WriteYAML(eventsDir, events.All); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, "gen-webhooks:", err)
		os.Exit(1)
	}
}
