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
)

const (
	// transcriptShadowBackfillMaxPage is the largest page Transcripts.Backfill accepts.
	// Each page is one transaction that seals every message in it.
	transcriptShadowBackfillMaxPage = 500

	// transcriptShadowBackfillDefaultPage keeps each page transaction short on
	// the primary while still finishing a project of a few hundred thousand
	// messages in a few thousand pages.
	transcriptShadowBackfillDefaultPage = 200
)

// requireTranscriptShadowProject reports whether projectID is on the capture
// allowlist, without touching the database or the encryption keys.
func requireTranscriptShadowProject(values []string, projectID uuid.UUID) error {
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return err
	}
	if !slices.Contains(projectIDs, projectID) {
		return fmt.Errorf("project must be listed in --%s", chatTranscriptShadowProjectIDsFlag)
	}
	return nil
}

// newTranscriptShadowBackfillCommand seals encrypted transcripts for a
// project's existing chat messages, so its chat reads can open them. Run it in a
// server pod, which has the organization encryption identity and settings.
func newTranscriptShadowBackfillCommand() *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "environment", EnvVars: []string{"GRAM_ENVIRONMENT"}, Required: true},
		&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
		&cli.StringFlag{Name: "project-id", Required: true, Usage: "Project whose messages to seal; it must already have encrypted transcripts"},
		&cli.StringFlag{Name: "after", Value: uuid.Nil.String(), Usage: "Resume after this message ID from a previous run"},
		&cli.IntFlag{Name: "page-size", Value: transcriptShadowBackfillDefaultPage, Usage: "Messages sealed per transaction, at most 500"},
		&cli.IntFlag{Name: "max-pages", Value: 0, Usage: "Stop after this many pages; 0 runs until the project is covered"},
		&cli.BoolFlag{Name: "apply", Usage: "Seal messages. Without it the command only reports coverage"},
		&cli.StringFlag{Name: "confirm-environment", Usage: "Must equal --environment when --apply is set"},
		transcriptShadowFlag(),
	}
	flags = append(flags, orgEncryptionFlags()...)

	return &cli.Command{
		Name:  "transcript-shadow-backfill",
		Usage: "Seal encrypted chat transcripts for a project's existing messages (dry run by default)",
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
			apply := c.Bool("apply")
			if apply && c.String("confirm-environment") != c.String("environment") {
				return errors.New("--apply requires --confirm-environment to equal --environment")
			}
			// Check the allowlist before connecting or provisioning any keys.
			// A project that does not capture does not seal new messages, and
			// body updates leave its transcripts stale, so coverage would break
			// after its next write.
			if err := requireTranscriptShadowProject(c.StringSlice(chatTranscriptShadowProjectIDsFlag), projectID); err != nil {
				return err
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
			// The same Transcripts the server builds at startup: it checks the
			// schema and provisions keys for the projects on the capture
			// allowlist.
			transcripts, err := newTranscripts(ctx, c, db, orgEncryption, otel.GetTracerProvider(), otel.GetMeterProvider())
			if err != nil {
				return err
			}

			out := c.App.Writer
			if err := writeTranscriptShadowProgress(ctx, out, transcripts, organizationID, projectID, "before"); err != nil {
				return err
			}
			if !apply {
				if _, err := fmt.Fprintln(out, "Dry run: nothing sealed. Rerun with --apply --confirm-environment to seal messages."); err != nil {
					return fmt.Errorf("write dry run result: %w", err)
				}
				return nil
			}

			run, err := backfillTranscriptShadows(ctx, transcripts, organizationID, projectID, after, int32(pageSize), maxPages)
			if writeErr := writeTranscriptShadowBackfillResult(out, run, err); writeErr != nil {
				return writeErr
			}
			if err != nil {
				return err
			}
			return writeTranscriptShadowProgress(ctx, out, transcripts, organizationID, projectID, "after")
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
func backfillTranscriptShadows(ctx context.Context, transcripts *transcriptstore.Transcripts, organizationID string, projectID, after uuid.UUID, pageSize int32, maxPages int) (transcriptShadowBackfillRun, error) {
	run := transcriptShadowBackfillRun{Sealed: 0, After: after, Complete: false}
	for pages := 0; maxPages == 0 || pages < maxPages; pages++ {
		page, err := backfillTranscriptShadowPage(ctx, transcripts, organizationID, projectID, run.After, pageSize)
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

func backfillTranscriptShadowPage(ctx context.Context, transcripts *transcriptstore.Transcripts, organizationID string, projectID, after uuid.UUID, pageSize int32) (transcriptstore.Page, error) {
	page, err := transcripts.Backfill(ctx, organizationID, projectID, after, pageSize)
	if err != nil {
		return transcriptstore.Page{After: after, Count: 0, More: true}, fmt.Errorf("seal encrypted transcript page: %w", err)
	}
	return page, nil
}

func writeTranscriptShadowProgress(ctx context.Context, out io.Writer, transcripts *transcriptstore.Transcripts, organizationID string, projectID uuid.UUID, label string) error {
	progress, err := transcripts.Progress(ctx, organizationID, projectID)
	if err != nil {
		return fmt.Errorf("read encrypted transcript coverage: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Coverage %s: current=%d waiting=%d unattributed=%d\n", label, progress.Current, progress.Waiting, progress.Unattributed); err != nil {
		return fmt.Errorf("write encrypted transcript coverage: %w", err)
	}
	return nil
}
