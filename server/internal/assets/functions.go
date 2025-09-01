package assets

import (
	"archive/zip"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

var functionsEntryPoints = map[string]struct{}{
	"functions.js":  {},
	"functions.mjs": {},
	"functions.ts":  {},
	"functions.mts": {},
	"functions.py":  {},
}

func validateFunctionsArchive(ctx context.Context, logger *slog.Logger, filename string) error {
	rdr, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return rdr.Close()
	})

	hasEntrypoint := slices.ContainsFunc(rdr.File, func(f *zip.File) bool {
		_, ok := functionsEntryPoints[f.Name]
		return ok
	})

	if !hasEntrypoint {
		return fmt.Errorf("validate functions archive: no entry point found (expected one of functions.{js,mjs,ts,mts,py})")
	}

	return nil
}
