// Command sweep_builtin_presets is an offline operator tool that re-evaluates
// stored risk findings against the built-in preset false-positive catalog in
// the internal/risk/presetlib package and marks the known-benign noise (test
// credit cards, example API keys/tokens, module hashes, placeholder emails,
// ...) as false positives so the dashboard hides them.
//
// It is the cross-source counterpart to the sweep_false_positives tool (which
// re-evaluates only Presidio PII findings against presidiofp). It is meant to
// be run rarely and by hand, against a production database reached through a
// Cloud SQL Auth Proxy tunnel:
//
//	cloud-sql-proxy --port 5432 <instance-connection-name> &
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	  go run ./server/cmd/tools/sweep_builtin_presets \
//	  -org org_123 -project <uuid> \
//	  -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
//	  -dry-run=false
//
// Safety properties:
//   - -dry-run defaults to true: a plain run only reports what it would mark.
//   - Every read and write is scoped by organization_id + project_id + id range
//     (and optionally policy), and the UPDATE re-checks false_positive_at IS NULL,
//     so re-runs and resumes are idempotent and cannot touch another tenant.
//   - false_positive_reason is stamped with the catalog version
//     ("preset:<version> <reason>") so a future targeted un-stamp can find
//     exactly the rows this tool wrote.
//   - Marking is reversible: UPDATE ... SET false_positive_at = NULL,
//     false_positive_reason = NULL over the same scope undoes a bad sweep.
//
// On interruption (Ctrl-C) or error it prints the last processed id so the run
// can be resumed with -cursor.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/sweep_builtin_presets/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

const (
	defaultBatchSize = 5000
	maxBatchSize     = 1_000_000
)

type config struct {
	dbURL     string
	orgID     string
	projectID uuid.UUID
	policyID  uuid.NullUUID
	from      time.Time
	to        time.Time
	cursor    uuid.UUID
	batchSize int32
	dryRun    bool
}

type report struct {
	scanned    int64
	flagged    int64
	updated    int64
	byReason   map[string]int64
	lastCursor uuid.UUID
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := parseFlags()
	if err != nil {
		log.Printf("invalid arguments: %v", err)
		return 2
	}

	lib, err := presetlib.New()
	if err != nil {
		log.Printf("load built-in exclusion library: %v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect database: %v", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Printf("ping database: %v", err)
		return 1
	}

	rep, runErr := sweep(ctx, pool, cfg, lib)
	printReport(cfg, rep, lib)

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Printf("sweep failed: %v", runErr)
		return 1
	}
	return 0
}

func parseFlags() (config, error) {
	var (
		dbURL     = flag.String("db", os.Getenv("GRAM_DATABASE_URL"), "Postgres connection string (defaults to $GRAM_DATABASE_URL)")
		orgID     = flag.String("org", "", "organization_id to scope the sweep (required)")
		projectID = flag.String("project", "", "project_id (uuid) to scope the sweep (required)")
		policyID  = flag.String("policy", "", "optional risk_policy_id (uuid) to scope to one policy; all policies if empty")
		fromStr   = flag.String("from", "", "lower time bound, RFC3339 (required); findings created at/after this are scanned")
		toStr     = flag.String("to", "", "upper time bound, RFC3339 (required); findings created before this are scanned")
		cursorStr = flag.String("cursor", "", "optional id to resume after (exclusive); overrides -from when set")
		batchSize = flag.Int("batch-size", defaultBatchSize, "requested rows per page (reads and writes capped at 5000)")
		dryRun    = flag.Bool("dry-run", true, "when true (default) only report; pass -dry-run=false to write")
	)
	flag.Parse()

	var cfg config
	cfg.dbURL = *dbURL
	cfg.orgID = *orgID
	cfg.dryRun = *dryRun

	if cfg.dbURL == "" {
		return cfg, errors.New("missing -db / $GRAM_DATABASE_URL")
	}
	if cfg.orgID == "" {
		return cfg, errors.New("missing -org")
	}

	pid, err := uuid.Parse(*projectID)
	if err != nil {
		return cfg, fmt.Errorf("invalid -project: %w", err)
	}
	cfg.projectID = pid

	if *policyID != "" {
		pol, err := uuid.Parse(*policyID)
		if err != nil {
			return cfg, fmt.Errorf("invalid -policy: %w", err)
		}
		cfg.policyID = uuid.NullUUID{UUID: pol, Valid: true}
	}

	cfg.from, err = time.Parse(time.RFC3339, *fromStr)
	if err != nil {
		return cfg, fmt.Errorf("invalid -from: %w", err)
	}
	cfg.to, err = time.Parse(time.RFC3339, *toStr)
	if err != nil {
		return cfg, fmt.Errorf("invalid -to: %w", err)
	}
	if !cfg.from.Before(cfg.to) {
		return cfg, errors.New("-from must be before -to")
	}

	if *batchSize <= 0 || *batchSize > maxBatchSize {
		return cfg, fmt.Errorf("-batch-size must be in [1, %d]", maxBatchSize)
	}
	cfg.batchSize = int32(*batchSize)

	// The keyset cursor is exclusive (id > cursor). Start just below the lower
	// time bound so the first real finding at -from is included; -cursor lets a
	// resumed run pick up where an interrupted one stopped.
	if *cursorStr != "" {
		cfg.cursor, err = uuid.Parse(*cursorStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
	} else {
		cfg.cursor = uuidv7.LowerBound(cfg.from)
	}

	return cfg, nil
}

// likeGlobs turns the catalog's "prefix*" rule-id globs into SQL LIKE patterns
// (e.g. "secret.stripe_*" -> "secret.stripe_%"). The LIKE clause is only a scan
// pre-filter: Library.Reason re-checks every fetched row, so a pattern that
// over-matches (e.g. an underscore acting as a LIKE single-char wildcard) can
// only widen the scan, never cause a wrong stamp.
func likeGlobs(globs []string) []string {
	if len(globs) == 0 {
		return nil
	}
	out := make([]string, len(globs))
	for i, g := range globs {
		out[i] = strings.TrimSuffix(g, "*") + "%"
	}
	return out
}

func sweep(ctx context.Context, pool *pgxpool.Pool, cfg config, lib *presetlib.Library) (report, error) {
	var rep report
	rep.byReason = map[string]int64{}
	rep.lastCursor = cfg.cursor

	ruleIDs := lib.RuleIDs()
	globs := likeGlobs(lib.RuleIDGlobs())
	provenance := "preset:" + lib.Version() + " "
	upper := uuidv7.LowerBound(cfg.to)
	cursor := cfg.cursor

	queries := repo.New(pool)
	// SQLc retains every match until the page is processed; cap the result
	// slice even when an operator requests a larger write batch.
	pageSize := min(cfg.batchSize, int32(defaultBatchSize))

	for {
		if err := ctx.Err(); err != nil {
			return rep, fmt.Errorf("sweep interrupted at %s: %w", cursor, err)
		}

		rows, err := queries.ListSweepCandidates(ctx, repo.ListSweepCandidatesParams{OrganizationID: cfg.orgID, ProjectID: cfg.projectID, PolicyID: cfg.policyID, RuleIds: ruleIDs, RuleGlobs: globs, Cursor: cursor, UpperBound: upper, PageSize: pageSize})
		if err != nil {
			return rep, fmt.Errorf("select page after %s: %w", cursor, err)
		}

		var (
			ids     []uuid.UUID
			reasons []string
			n       int
		)
		for _, row := range rows {
			id, source, ruleID, match := row.ID, row.Source, row.RuleID.String, conv.FromPGText[string](row.Match)
			n++
			cursor = id
			if match == nil {
				continue
			}
			if reason := lib.Reason(presetlib.Match{Source: source, RuleID: ruleID, Value: *match}); reason != "" {
				stamped := provenance + reason
				ids = append(ids, id)
				reasons = append(reasons, stamped)
				rep.byReason[reason]++
			}
		}

		rep.scanned += int64(n)
		rep.flagged += int64(len(ids))

		if len(ids) > 0 && !cfg.dryRun {
			updated, err := queries.MarkSweepBatch(ctx, repo.MarkSweepBatchParams{Ids: ids, Reasons: reasons, ProjectID: cfg.projectID, OrganizationID: cfg.orgID})
			if err != nil {
				// Do NOT advance rep.lastCursor: this batch's rows were not
				// stamped, so a resume must re-scan from the previous cursor.
				return rep, fmt.Errorf("mark batch ending at %s: %w", cursor, err)
			}
			rep.updated += updated
		}

		// Advance the resume cursor only after the batch's write has succeeded
		// (dry runs perform no write). Advancing earlier would report a cursor
		// past rows that were never stamped, skipping them on retry.
		rep.lastCursor = cursor

		log.Printf("scanned=%d flagged=%d updated=%d cursor=%s",
			rep.scanned, rep.flagged, rep.updated, rep.lastCursor)

		// A short page means we reached the end of the window.
		if n < int(pageSize) {
			return rep, nil
		}
	}
}

func printReport(cfg config, rep report, lib *presetlib.Library) {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("preset false-positive sweep summary")
	fmt.Printf("  catalog:     preset:%s\n", lib.Version())
	fmt.Printf("  mode:        %s\n", mode)
	fmt.Printf("  org:         %s\n", cfg.orgID)
	fmt.Printf("  project:     %s\n", cfg.projectID)
	if cfg.policyID.Valid {
		fmt.Printf("  policy:      %s\n", cfg.policyID.UUID)
	} else {
		fmt.Printf("  policy:      (all)\n")
	}
	fmt.Printf("  window:      %s .. %s\n", cfg.from.Format(time.RFC3339), cfg.to.Format(time.RFC3339))
	fmt.Printf("  scanned:     %d\n", rep.scanned)
	fmt.Printf("  flagged:     %d\n", rep.flagged)
	fmt.Printf("  updated:     %d\n", rep.updated)
	fmt.Printf("  last cursor: %s\n", rep.lastCursor)

	if len(rep.byReason) > 0 {
		fmt.Println("  by reason:")
		reasons := make([]string, 0, len(rep.byReason))
		for r := range rep.byReason {
			reasons = append(reasons, r)
		}
		sort.Slice(reasons, func(i, j int) bool {
			if rep.byReason[reasons[i]] != rep.byReason[reasons[j]] {
				return rep.byReason[reasons[i]] > rep.byReason[reasons[j]]
			}
			return reasons[i] < reasons[j]
		})
		for _, r := range reasons {
			fmt.Printf("    %6d  %s\n", rep.byReason[r], r)
		}
	}
	if cfg.dryRun && rep.flagged > 0 {
		fmt.Println()
		fmt.Println("re-run with -dry-run=false to apply.")
	}
}
