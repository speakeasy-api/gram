package gram

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// intentImportDefaultLimit keeps the session picker usable after an import.
const intentImportDefaultLimit = 150

// intentImportNamespace derives stable chat ids from titles so reruns skip
// transcripts that are already imported.
var intentImportNamespace = uuid.MustParse("6f1c7a52-2c1e-4f4e-9d1a-8a0c3f6b9e11")

func newIntentPoliciesImportCommand() *cli.Command {
	return &cli.Command{
		Name:  "intent-policies-import",
		Usage: "Local development only: import a directory of agent transcripts as chats in the local dev project",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Usage: "Directory of transcripts, one subdirectory per source", Required: true},
			&cli.IntFlag{Name: "limit", Usage: "Maximum transcripts to import, taken round-robin across sources", Value: intentImportDefaultLimit},
			&cli.StringFlag{Name: "database-url", Usage: "Database URL", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
			&cli.StringFlag{Name: "environment", Usage: "Deployment environment; must be local", EnvVars: []string{"GRAM_ENVIRONMENT"}, Value: "local"},
		},
		Action: func(c *cli.Context) error {
			// The flag's default must not satisfy the guard.
			if !c.IsSet("environment") || c.String("environment") != "local" {
				return errors.New("intent-policies-import only runs against the local environment")
			}
			ctx := c.Context
			logger := PullLogger(ctx)

			files, err := intentImportFiles(c.String("dir"), c.Int("limit"))
			if err != nil {
				return err
			}

			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{enableUnsafeLogging: false})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			spec := demoseed.LocalSpec()
			projectID, err := uuid.Parse(spec.ProjectID())
			if err != nil {
				return fmt.Errorf("parse local project id: %w", err)
			}
			// Imports into the local demo project only, which has no encrypted
			// transcripts, so nothing is sealed.
			writer, shutdown := chat.NewChatMessageWriter(logger, db, nil, nil)
			defer o11y.NoLogDefer(func() error { return shutdown(ctx) })

			imported, skipped := 0, 0
			for _, f := range files {
				title := filepath.Base(filepath.Dir(f)) + "/" + filepath.Base(f)
				chatID := uuid.NewSHA1(intentImportNamespace, []byte(projectID.String()+"/"+title))
				// Counts messages of soft-deleted chats too, which GetChat hides.
				existing, err := chatrepo.New(db).CountChatMessages(ctx, chatrepo.CountChatMessagesParams{ChatID: chatID, ProjectID: projectID})
				if err != nil {
					return fmt.Errorf("check existing chat %s: %w", title, err)
				}
				if existing > 0 {
					skipped++
					continue
				}

				info, err := os.Stat(f)
				if err != nil {
					return fmt.Errorf("stat %s: %w", f, err)
				}
				if info.Size() > intentpolicy.MaxTranscriptBytes {
					logger.WarnContext(ctx, "skip oversized transcript", attr.SlogFilePath(f))
					continue
				}
				raw, err := os.ReadFile(f) //nolint:gosec // local developer-supplied directory
				if err != nil {
					return fmt.Errorf("read %s: %w", f, err)
				}
				transcript, err := intentpolicy.ParseTranscript(raw)
				if err != nil {
					logger.WarnContext(ctx, "skip unparseable transcript", attr.SlogFilePath(f), attr.SlogError(err))
					continue
				}
				if _, err := intentpolicy.StoreTranscript(ctx, writer, intentpolicy.StoreParams{
					ChatID:         chatID,
					ProjectID:      projectID,
					OrganizationID: spec.OrgID,
					UserID:         "",
					Title:          title,
					Source:         intentpolicy.SourceImport,
				}, transcript, time.Now()); err != nil {
					return fmt.Errorf("store %s: %w", title, err)
				}
				imported++
			}
			logger.InfoContext(ctx, fmt.Sprintf("imported %d transcripts, skipped %d already present", imported, skipped))
			return nil
		},
	}
}

// intentImportFiles lists transcripts per source subdirectory and interleaves
// them so a limited import still covers every source.
func intentImportFiles(dir string, limit int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var sources [][]string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, e.Name(), "*.json*"))
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", e.Name(), err)
		}
		slices.Sort(matches)
		if len(matches) > 0 {
			sources = append(sources, matches)
		}
	}

	var files []string
	for i := 0; len(files) < limit; i++ {
		added := false
		for _, s := range sources {
			if i < len(s) && len(files) < limit {
				files = append(files, s[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return files, nil
}
