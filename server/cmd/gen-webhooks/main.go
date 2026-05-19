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
	"os/exec"

	"github.com/speakeasy-api/gram/server/internal/outbox/cataloggen"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// eventsDir is relative to the server/ directory where the mise task runs.
const eventsDir = "./internal/outbox/events"

func main() {
	check := flag.Bool("check", false, "verify both generated files are up to date without writing")
	yamlOnly := flag.Bool("yaml-only", false, "only generate catalog_gen.yaml; skip catalog_gen.go")
	flag.Parse()

	var errs []error
	switch {
	case *check:
		if err := cataloggen.Check(eventsDir); err != nil {
			errs = append(errs, err)
		}
		if err := cataloggen.CheckYAML(eventsDir, events.All); err != nil {
			errs = append(errs, err)
		}
	case *yamlOnly:
		// Invoked as a subprocess after catalog_gen.go has been rewritten so
		// that events.All in this binary reflects the updated file.
		if err := cataloggen.WriteYAML(eventsDir, events.All); err != nil {
			errs = append(errs, err)
		}
	default:
		if err := cataloggen.Write(eventsDir); err != nil {
			errs = append(errs, err)
		}
		// catalog_gen.go was just rewritten; events.All in this binary was
		// compiled from the previous version and is now stale. Rerun as a
		// subprocess so Go recompiles the events package before generating YAML.
		cmd := exec.Command("go", "run", "./cmd/gen-webhooks", "--yaml-only")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("generate yaml: %w", err))
		}
	}

	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, "gen-webhooks:", err)
		os.Exit(1)
	}
}
