package functions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
)

// ErrNoDevScript means package.json has no dev script to run.
var ErrNoDevScript = errors.New(`package.json has no "dev" script: add one that starts your function locally, for example ` +
	`"dev": "mcp-inspector --transport stdio node ./src/server.ts"`)

// Dev runs the project's dev package script with the project's package
// manager, forwarding extra arguments to it.
func (r Runner) Dev(ctx context.Context, dir string, extra []string) error {
	pkg, err := readPackageJSON(filepath.Join(dir, "package.json"))
	if err != nil {
		return err
	}
	if _, ok := pkg.scripts()["dev"]; !ok {
		return ErrNoDevScript
	}

	pm := DetectPackageManager(dir, r.Getenv("npm_config_user_agent"))
	cmd, err := r.command(context.WithoutCancel(ctx), dir, pm, runScriptArgs(pm, "dev", extra)...)
	if err != nil {
		return fmt.Errorf("run the dev script with %s: %w", pm, err)
	}

	// The terminal sends Ctrl-C to the whole process group. Catch it here so
	// the CLI waits for the dev server to shut down instead of exiting first.
	// A caught signal is reset to its default in the child, unlike an ignored
	// one.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run dev: %w", pm, err)
	}
	return nil
}
