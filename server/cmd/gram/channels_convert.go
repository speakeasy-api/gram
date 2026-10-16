package gram

import (
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/channels"
)

// newChannelsConvertCommand exposes the Channels proof-of-concept conversion
// as an explicit, reviewable operation rather than a function nobody can run.
// It is deliberately a separate command and not part of startup: converting is
// an operator decision, and it rewrites identifiers.
func newChannelsConvertCommand() *cli.Command {
	return &cli.Command{
		Name:  "channels-convert",
		Usage: "Copy the superseded Channels state document into the normalized Channels tables (idempotent; reports the identifier mapping)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "organization",
				Usage:   "Convert only this organization. Required unless --all-organizations is given.",
				EnvVars: []string{"GRAM_CHANNELS_CONVERT_ORGANIZATION"},
			},
			&cli.BoolFlag{
				Name: "all-organizations",
				Usage: "Convert every organization with a legacy document in this database. " +
					"Without it an organization must be named, so a mistyped database URL cannot write across tenants.",
				EnvVars: []string{"GRAM_CHANNELS_CONVERT_ALL_ORGANIZATIONS"},
			},
			&cli.BoolFlag{
				Name: "apply",
				Usage: "Write the conversion. Without it the whole conversion runs and is rolled back, " +
					"so the counts and the identifier mapping below are a report of what would happen.",
				EnvVars: []string{"GRAM_CHANNELS_CONVERT_APPLY"},
			},
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := PullLogger(ctx)

			organization := c.String("organization")
			all := c.Bool("all-organizations")
			switch {
			case organization == "" && !all:
				return errors.New("name an organization with --organization, or pass --all-organizations to convert every tenant in this database")
			case organization != "" && all:
				return errors.New("--organization and --all-organizations are mutually exclusive")
			}

			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: false,
			})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			apply := c.Bool("apply")
			result, convertErr := channels.ConvertLegacyState(ctx, db, logger, channels.ConversionOptions{
				DryRun:         !apply,
				OrganizationID: organization,
			})
			// A refusal still produces a report: the operator needs to see
			// what was converted and what was not before deciding anything.

			// The mapping goes to stdout because it is the command's product:
			// an operator pastes it to whoever holds a stale identifier.
			// Identifiers only — see writeChannelsConversionMapping.
			if err := writeChannelsConversionMapping(os.Stdout, result); err != nil {
				return err
			}
			if convertErr != nil {
				return fmt.Errorf("convert channels state: %w", convertErr)
			}
			if apply {
				logger.InfoContext(ctx, "channels conversion applied",
					attr.SlogDBUpdatedRowsCount(int64(result.Messages)))
				return nil
			}
			logger.InfoContext(ctx, "channels conversion was rolled back; re-run with --apply to write it",
				attr.SlogDBUpdatedRowsCount(int64(result.Messages)))
			return nil
		},
	}
}

// writeChannelsConversionMapping prints the old-to-new identifier mapping.
//
// It deliberately omits the instance label. The mapping exists so an operator
// can translate a stale identifier, which needs identifiers and nothing else,
// and this output is meant to be copied to other people. A label is the
// authenticated account's display name for a browser session and agent-chosen
// text otherwise, and the superseded documents this command reads were written
// before labels stopped being built from the account's email address — so
// printing them here would reintroduce, on a surface designed for copying, the
// exposure the authenticator now avoids. The organization identifier stays
// because a mapping spanning tenants is ambiguous without it.
func writeChannelsConversionMapping(w io.Writer, result channels.ConversionResult) error {
	out := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(out, "organizations\t%d\ninstances\t%d\nconversations\t%d\nmessages\t%d\nskipped messages\t%d\n\n",
		result.Organizations, result.Instances, result.Conversations, result.Messages, result.SkippedMessages); err != nil {
		return fmt.Errorf("write channels conversion summary: %w", err)
	}
	if len(result.Refused) > 0 {
		if _, err := fmt.Fprintln(out, "REFUSED ORGANIZATION\tREASON"); err != nil {
			return fmt.Errorf("write channels conversion refusals header: %w", err)
		}
		for _, refusal := range result.Refused {
			if _, err := fmt.Fprintf(out, "%s\t%s\n", refusal.OrganizationID, refusal.Reason); err != nil {
				return fmt.Errorf("write channels conversion refusal: %w", err)
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return fmt.Errorf("write channels conversion refusals separator: %w", err)
		}
	}
	if len(result.Mappings) > 0 {
		if _, err := fmt.Fprintln(out, "ORGANIZATION\tKIND\tOLD ID\tNEW ID"); err != nil {
			return fmt.Errorf("write channels conversion mapping header: %w", err)
		}
		for _, mapping := range result.Mappings {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\n",
				mapping.OrganizationID, mapping.Kind, mapping.LegacyID, mapping.ID); err != nil {
				return fmt.Errorf("write channels conversion mapping: %w", err)
			}
		}
	}
	if err := out.Flush(); err != nil {
		return fmt.Errorf("flush channels conversion mapping: %w", err)
	}
	return nil
}
