package gram

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
)

// Replay is explicitly scoped and bounded. Re-running a window is safe for
// distinct session/message aggregates; it never re-meters or re-scans content.
func newReplaySessionObservationsCommand() *cli.Command {
	return &cli.Command{
		Name:  "replay-session-observations",
		Usage: "Queue historical imported sessions for analytics (one bounded batch)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
			&cli.StringFlag{Name: "project-id", Required: true},
			&cli.TimestampFlag{Name: "from", Layout: time.RFC3339, Required: true},
			&cli.TimestampFlag{Name: "to", Layout: time.RFC3339, Required: true},
			&cli.StringFlag{Name: "after", Value: uuid.Nil.String(), Usage: "Resume after the message ID from the previous batch"},
			&cli.IntFlag{Name: "limit", Value: 1000},
		},
		Action: func(c *cli.Context) error {
			projectID, err := uuid.Parse(c.String("project-id"))
			if err != nil {
				return fmt.Errorf("parse project ID: %w", err)
			}
			after, err := uuid.Parse(c.String("after"))
			if err != nil {
				return fmt.Errorf("parse cursor: %w", err)
			}
			limit := c.Int("limit")
			from, to := c.Timestamp("from"), c.Timestamp("to")
			if limit < 1 || limit > 10000 || from == nil || to == nil || !from.Before(*to) {
				return fmt.Errorf("require from < to and limit between 1 and 10000")
			}
			db, err := newDBClient(c.Context, PullLogger(c.Context), otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{enableUnsafeLogging: false})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()
			rows, err := chatrepo.New(db).ListImportedSessionObservationReplay(c.Context, chatrepo.ListImportedSessionObservationReplayParams{
				ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, FromTime: conv.ToPGTimestamptz(*from), ToTime: conv.ToPGTimestamptz(*to), AfterID: after, RowLimit: int32(limit),
			})
			if err != nil {
				return fmt.Errorf("list imported messages: %w", err)
			}
			batch := make([]outbox.Message, 0, len(rows))
			for _, row := range rows {
				batch = append(batch, outbox.Message{Proto: telemetryv1.SessionObserved_builder{ProjectId: new(projectID.String()), MessageId: new(row.ID.String())}.Build(), PublicID: uuid.Nil, Attributes: nil})
			}
			if len(batch) == 0 {
				if _, err := fmt.Fprintln(c.App.Writer, "No remaining imported messages in this window."); err != nil {
					return fmt.Errorf("write replay result: %w", err)
				}
				return nil
			}
			if _, err := outbox.PublishBatch(c.Context, db, rows[0].OrganizationID, batch); err != nil {
				return fmt.Errorf("enqueue session observations: %w", err)
			}
			if _, err := fmt.Fprintf(c.App.Writer, "Queued %d messages; resume with --after %s using the same project and window.\n", len(rows), rows[len(rows)-1].ID); err != nil {
				return fmt.Errorf("write replay cursor: %w", err)
			}
			return nil
		},
	}
}
