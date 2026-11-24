package gram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

const (
	// transcriptShadowBackfillMaxPage is the largest page Store.Backfill accepts.
	// Each page is one transaction that seals every message in it.
	transcriptShadowBackfillMaxPage = 500

	// transcriptShadowBackfillDefaultPage keeps each page transaction short on
	// the primary while still finishing a project of a few hundred thousand
	// messages in a few thousand pages.
	transcriptShadowBackfillDefaultPage = 200

	// transcriptShadowBackfillChangedRetries is how many times a page is retried
	// when a message changes while it is being sealed. Live projects write
	// constantly, so a page can lose a race; a run that keeps losing stops
	// instead of spinning.
	transcriptShadowBackfillChangedRetries = 5
)

// newTranscriptShadowBackfillCommand seals encrypted shadows for a project's
// existing chat messages, so its chat reads can open shadows. Run it in a
// server pod, which has the organization encryption identity and settings.
func newTranscriptShadowBackfillCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "environment", EnvVars: []string{"GRAM_ENVIRONMENT"}, Required: true},
		&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
		&cli.StringFlag{Name: "project-id", Required: true, Usage: "Project whose messages to seal; it must already capture shadows"},
		&cli.StringFlag{Name: "after", Value: uuid.Nil.String(), Usage: "Resume after this message ID from a previous run"},
		&cli.IntFlag{Name: "page-size", Value: transcriptShadowBackfillDefaultPage, Usage: "Messages sealed per transaction, at most 500"},
		&cli.IntFlag{Name: "max-pages", Value: 0, Usage: "Stop after this many pages; 0 runs until the project is covered"},
		&cli.BoolFlag{Name: "apply", Usage: "Seal shadows. Without it the command only reports coverage"},
		&cli.StringFlag{Name: "confirm-environment", Usage: "Must equal --environment when --apply is set"},
		transcriptShadowFlag(),
	}
	flags = append(flags, orgEncryptionFlags()...)

	return &cli.Command{
		Name:  "transcript-shadow-backfill",
		Usage: "Seal encrypted chat transcript shadows for a project's existing messages (dry run by default)",
		Flags: flags,
		Action: func(c *cli.Context) error {
			ctx := c.Context
			projectID, err := uuid.Parse(c.String("project-id"))
			if err != nil || projectID == uuid.Nil {
				return errors.New("parse project ID")
			}
			after, err := uuid.Parse(c.String("after"))
			if err != nil {
				return errors.New("parse resume cursor")
			}
			pageSize := c.Int("page-size")
			if pageSize < 1 || pageSize > transcriptShadowBackfillMaxPage {
				return fmt.Errorf("page-size must be between 1 and %d", transcriptShadowBackfillMaxPage)
			}
			maxPages := c.Int("max-pages")
			if maxPages < 0 {
				return errors.New("max-pages must not be negative")
			}
			// Sealing copies for a project that does not capture would leave
			// shadows that go stale on its next write and encrypt data nobody opted in.
			captureProjectIDs, err := parseTranscriptShadowProjectIDs(c.StringSlice(chatTranscriptShadowProjectIDsFlag))
			if err != nil {
				return err
			}
			if !slices.Contains(captureProjectIDs, projectID) {
				return fmt.Errorf("project must be listed in --%s", chatTranscriptShadowProjectIDsFlag)
			}
			apply := c.Bool("apply")
			if apply && c.String("confirm-environment") != c.String("environment") {
				return errors.New("--apply requires --confirm-environment to equal --environment")
			}

			logger := PullLogger(ctx)
			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{enableUnsafeLogging: false})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			organizationID, err := chatrepo.New(db).GetProjectOrganizationID(ctx, projectID)
			if err != nil {
				return fmt.Errorf("resolve project organization: %w", err)
			}
			orgEncryption, err := newOrgEncryption(ctx, logger, c, db, newGCPIdentity(ctx, logger, c), newAuditLogger())
			if err != nil {
				return err
			}
			if orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
				return errors.New("transcript shadow backfill requires organization encryption")
			}
			store, err := transcriptstore.New(db, orgEncryption.Service, otel.GetTracerProvider(), otel.GetMeterProvider())
			if err != nil {
				return fmt.Errorf("create transcript shadow store: %w", err)
			}

			out := c.App.Writer
			if err := writeTranscriptShadowProgress(ctx, out, store, organizationID, projectID, "before"); err != nil {
				return err
			}
			if !apply {
				if _, err := fmt.Fprintln(out, "Dry run: nothing sealed. Rerun with --apply --confirm-environment to seal shadows."); err != nil {
					return fmt.Errorf("write dry run result: %w", err)
				}
				return nil
			}

			if err := orgEncryption.Provisioner.EnsureUseCase(ctx, organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
				return fmt.Errorf("provision transcript encryption: %w", err)
			}
			run, err := backfillTranscriptShadows(ctx, store, organizationID, projectID, after, int32(pageSize), maxPages)
			if writeErr := writeTranscriptShadowBackfillResult(out, run, err); writeErr != nil {
				return writeErr
			}
			if err != nil {
				return err
			}
			return writeTranscriptShadowProgress(ctx, out, store, organizationID, projectID, "after")
		},
	}
}

// transcriptShadowBackfillRun is the result of one backfill run.
type transcriptShadowBackfillRun struct {
	// Sealed is how many messages the run sealed.
	Sealed int

	// After is the last message ID the run covered, the cursor to resume from.
	After uuid.UUID

	// Complete is true when the sweep reached the end of the project. A
	// complete sweep's cursor is not a resume point: messages inserted below
	// it since the sweep began would be skipped.
	Complete bool
}

// backfillTranscriptShadows seals pages until the project is covered or
// maxPages pages are sealed (0 means no limit). It returns the run, including
// the cursor to resume from when it fails or stops early.
func backfillTranscriptShadows(ctx context.Context, store *transcriptstore.Store, organizationID string, projectID, after uuid.UUID, pageSize int32, maxPages int) (transcriptShadowBackfillRun, error) {
	run := transcriptShadowBackfillRun{Sealed: 0, After: after, Complete: false}
	for pages := 0; maxPages == 0 || pages < maxPages; pages++ {
		page, err := backfillTranscriptShadowPage(ctx, store, organizationID, projectID, run.After, pageSize)
		if err != nil {
			return run, err
		}
		run.Sealed += page.Count
		run.After = page.After
		if !page.More {
			run.Complete = true
			return run, nil
		}
	}
	return run, nil
}

// writeTranscriptShadowBackfillResult prints the resume hint only for a run
// that stopped early, since a complete sweep's cursor would skip messages.
func writeTranscriptShadowBackfillResult(out io.Writer, run transcriptShadowBackfillRun, runErr error) error {
	if run.Complete && runErr == nil {
		if _, err := fmt.Fprintf(out, "Sealed %d messages; the sweep reached the end of the project.\n", run.Sealed); err != nil {
			return fmt.Errorf("write backfill result: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(out, "Sealed %d messages; resume with --after %s.\n", run.Sealed, run.After); err != nil {
		return fmt.Errorf("write backfill cursor: %w", err)
	}
	return nil
}

func backfillTranscriptShadowPage(ctx context.Context, store *transcriptstore.Store, organizationID string, projectID, after uuid.UUID, pageSize int32) (transcriptstore.Page, error) {
	for attempt := 1; ; attempt++ {
		page, err := store.Backfill(ctx, organizationID, projectID, after, pageSize)
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, transcriptstore.ErrChanged) || attempt >= transcriptShadowBackfillChangedRetries {
			return transcriptstore.Page{After: after, Count: 0, More: true}, fmt.Errorf("seal transcript shadow page: %w", err)
		}
	}
}

func writeTranscriptShadowProgress(ctx context.Context, out io.Writer, store *transcriptstore.Store, organizationID string, projectID uuid.UUID, label string) error {
	progress, err := store.Progress(ctx, organizationID, projectID)
	if err != nil {
		return fmt.Errorf("read transcript shadow coverage: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Coverage %s: current=%d waiting=%d unattributed=%d\n", label, progress.Current, progress.Waiting, progress.Unattributed); err != nil {
		return fmt.Errorf("write transcript shadow coverage: %w", err)
	}
	return nil
}
