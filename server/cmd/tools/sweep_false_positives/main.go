// Command sweep_false_positives is an offline operator tool that re-evaluates
// stored Presidio risk findings against the false-positive catalogs in the
// internal/risk/presidiofp package and marks the noise (reserved IPs, placeholder emails,
// retired recognizers, NHS numbers with no health-care context, ...) as false
// positives so the dashboard hides them.
//
// Most catalogs judge the matched value alone, so a stored row carries
// everything they need. The NHS catalog also has a context layer — a ten-digit
// run is only an NHS number if the payload it came from mentions health care —
// which the live scanners evaluate against the text they scan. To let history
// reach the same verdict, the sweep re-reads the chat message a candidate row
// was anchored to and hands that text to the same classifier. Pass
// -message-context=false to skip the re-read and sweep on values alone.
//
// It is meant to be run rarely and by hand, against a production database
// reached through a Cloud SQL Auth Proxy tunnel:
//
//	cloud-sql-proxy --port 5432 <instance-connection-name> &
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	  go run ./server/cmd/tools/sweep_false_positives \
//	  -org org_123 -project <uuid> \
//	  -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
//	  -dry-run=false
//
// Safety properties:
//   - -dry-run defaults to true: a plain run only reports what it would mark.
//   - Every read and write is scoped by organization_id + project_id + id range
//     (and optionally policy), and the UPDATE re-checks false_positive_at IS NULL,
//     so re-runs and resumes are idempotent and cannot touch another tenant.
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
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/risk/presidiofp"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

const (
	defaultBatchSize = 5000
	maxBatchSize     = 1_000_000
)

type config struct {
	dbURL          string
	orgID          string
	projectID      uuid.UUID
	policyID       uuid.NullUUID
	from           time.Time
	to             time.Time
	cursor         uuid.UUID
	batchSize      int32
	dryRun         bool
	messageContext bool
}

type report struct {
	scanned    int64
	flagged    int64
	updated    int64
	byReason   map[string]int64
	lastCursor uuid.UUID

	// contextRead counts rows the sweep re-read a message for; contextMissing
	// counts rows a context rule could have judged but whose payload could not
	// be recovered (content-part anchors, deleted messages).
	contextRead    int64
	contextMissing int64
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

	rep, runErr := sweep(ctx, pool, cfg)
	printReport(cfg, rep)

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
		batchSize = flag.Int("batch-size", defaultBatchSize, "rows fetched and updated per page")
		dryRun    = flag.Bool("dry-run", true, "when true (default) only report; pass -dry-run=false to write")
		msgCtx    = flag.Bool("message-context", true, "re-read the anchoring chat message so catalogs that classify on surrounding text (NHS numbers) apply; pass -message-context=false for a value-only sweep")
	)
	flag.Parse()

	var cfg config
	cfg.dbURL = *dbURL
	cfg.orgID = *orgID
	cfg.dryRun = *dryRun
	cfg.messageContext = *msgCtx

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

// selectPage walks risk_results in id order. It only fetches rows whose rule_id
// has a false-positive catalog (presidiofp.RuleIDs) and that are still active
// (found, not excluded, not already swept), within the id/time window.
//
// chat_message_id comes along for the context pass: rules like the NHS one need
// the text the match was found in, which is re-read from the anchoring message
// (see selectMessages). Rows anchored to a content part instead carry NULL and
// simply never get a context verdict.
const selectPage = `
SELECT id, rule_id, match, chat_message_id
FROM risk_results
WHERE organization_id = $1
  AND project_id = $2
  AND ($3::uuid IS NULL OR risk_policy_id = $3)
  AND found IS TRUE
  AND excluded_at IS NULL
  AND false_positive_at IS NULL
  AND rule_id = ANY($4::text[])
  AND id > $5
  AND id < $6
ORDER BY id
LIMIT $7
`

// selectMessages re-reads the payload a finding was scanned in, for the
// catalogs that classify on surrounding text rather than on the matched value.
//
// Both the message body and its tool calls are concatenated because a finding
// can sit in either, and the context catalogs only ever suppress on the
// *absence* of a signal — handing them more text can only keep more findings.
// The per-column cap keeps a pathological message from dominating a page; the
// prefix is far longer than any payload the scanner accepts.
//
// The project_id predicate is redundant — these ids came from risk_results rows
// already scoped to the project — but it keeps the tool's "every read is tenant
// scoped" property true of each statement on its own.
const selectMessages = `
SELECT id
     , left(content, 262144) || ' ' || left(coalesce(tool_calls::text, ''), 262144)
FROM chat_messages
WHERE project_id = $1
  AND id = ANY($2::uuid[])
`

// markBatch flags the accumulated false positives. The id/reason pairs arrive
// as parallel arrays; the false_positive_at IS NULL recheck keeps it idempotent.
const markBatch = `
UPDATE risk_results r
SET false_positive_at = now()
  , false_positive_reason = t.reason
FROM unnest($1::uuid[], $2::text[]) AS t(id, reason)
WHERE r.id = t.id
  AND r.false_positive_at IS NULL
`

// candidate is a row that the value-only pass did not classify but whose rule
// could still be resolved once the surrounding text is known.
type candidate struct {
	id        uuid.UUID
	ruleID    string
	match     string
	messageID uuid.UUID
}

func sweep(ctx context.Context, pool *pgxpool.Pool, cfg config) (report, error) {
	var rep report
	rep.byReason = map[string]int64{}
	rep.lastCursor = cfg.cursor

	ruleIDs := presidiofp.RuleIDs()
	contextRules := map[string]struct{}{}
	if cfg.messageContext {
		for _, id := range presidiofp.ContextRuleIDs() {
			contextRules[id] = struct{}{}
		}
	}
	upper := uuidv7.LowerBound(cfg.to)
	cursor := cfg.cursor

	var policyArg any
	if cfg.policyID.Valid {
		policyArg = cfg.policyID.UUID
	}

	for {
		if err := ctx.Err(); err != nil {
			return rep, fmt.Errorf("sweep interrupted at %s: %w", cursor, err)
		}

		rows, err := pool.Query(ctx, selectPage,
			cfg.orgID, cfg.projectID, policyArg, ruleIDs, cursor, upper, cfg.batchSize)
		if err != nil {
			return rep, fmt.Errorf("select page after %s: %w", cursor, err)
		}

		var (
			ids        []uuid.UUID
			reasons    []string
			candidates []candidate
			n          int
		)
		for rows.Next() {
			var (
				id        uuid.UUID
				ruleID    string
				match     *string
				messageID uuid.NullUUID
			)
			if err := rows.Scan(&id, &ruleID, &match, &messageID); err != nil {
				rows.Close()
				return rep, fmt.Errorf("scan row: %w", err)
			}
			n++
			cursor = id
			if match == nil {
				continue
			}
			if reason := presidiofp.ReasonByRuleID(ruleID, *match); reason != "" {
				ids = append(ids, id)
				reasons = append(reasons, reason)
				rep.byReason[reason]++
				continue
			}
			if _, needsContext := contextRules[ruleID]; !needsContext {
				continue
			}
			if !messageID.Valid {
				// Anchored to a content part, whose text lives in the asset
				// store rather than a column. No context, no verdict.
				rep.contextMissing++
				continue
			}
			candidates = append(candidates, candidate{
				id:        id,
				ruleID:    ruleID,
				match:     *match,
				messageID: messageID.UUID,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return rep, fmt.Errorf("iterate page: %w", err)
		}
		rows.Close()

		if len(candidates) > 0 {
			texts, err := loadMessageTexts(ctx, pool, cfg.projectID, candidates)
			if err != nil {
				return rep, fmt.Errorf("load message context for page ending at %s: %w", cursor, err)
			}
			for _, c := range candidates {
				text, ok := texts[c.messageID]
				if !ok {
					// The message was deleted out from under the finding.
					rep.contextMissing++
					continue
				}
				rep.contextRead++
				reason := presidiofp.ReasonByRuleIDInContext(c.ruleID, c.match, text)
				if reason == "" {
					continue
				}
				ids = append(ids, c.id)
				reasons = append(reasons, reason)
				rep.byReason[reason]++
			}
		}

		rep.scanned += int64(n)
		rep.flagged += int64(len(ids))
		rep.lastCursor = cursor

		if len(ids) > 0 && !cfg.dryRun {
			tag, err := pool.Exec(ctx, markBatch, ids, reasons)
			if err != nil {
				return rep, fmt.Errorf("mark batch ending at %s: %w", cursor, err)
			}
			rep.updated += tag.RowsAffected()
		}

		log.Printf("scanned=%d flagged=%d updated=%d cursor=%s",
			rep.scanned, rep.flagged, rep.updated, rep.lastCursor)

		// A short page means we reached the end of the window.
		if n < int(cfg.batchSize) {
			return rep, nil
		}
	}
}

// loadMessageTexts re-reads the payloads the given candidates were scanned in,
// keyed by message id. Several findings usually share one message, so the ids
// are deduped before the query. A message that no longer exists is simply
// absent from the result, and its candidates go unclassified.
func loadMessageTexts(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID, candidates []candidate) (map[uuid.UUID]string, error) {
	seen := make(map[uuid.UUID]struct{}, len(candidates))
	messageIDs := make([]uuid.UUID, 0, len(candidates))
	for _, c := range candidates {
		if _, dup := seen[c.messageID]; dup {
			continue
		}
		seen[c.messageID] = struct{}{}
		messageIDs = append(messageIDs, c.messageID)
	}

	rows, err := pool.Query(ctx, selectMessages, projectID, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string, len(messageIDs))
	for rows.Next() {
		var (
			id   uuid.UUID
			text string
		)
		if err := rows.Scan(&id, &text); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		out[id] = text
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return out, nil
}

func printReport(cfg config, rep report) {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("false-positive sweep summary")
	fmt.Printf("  mode:        %s\n", mode)
	fmt.Printf("  org:         %s\n", cfg.orgID)
	fmt.Printf("  project:     %s\n", cfg.projectID)
	if cfg.policyID.Valid {
		fmt.Printf("  policy:      %s\n", cfg.policyID.UUID)
	} else {
		fmt.Printf("  policy:      (all)\n")
	}
	fmt.Printf("  window:      %s .. %s\n", cfg.from.Format(time.RFC3339), cfg.to.Format(time.RFC3339))
	if cfg.messageContext {
		fmt.Printf("  context:     on (re-reads the anchoring message)\n")
	} else {
		fmt.Printf("  context:     off (value-only)\n")
	}
	fmt.Printf("  scanned:     %d\n", rep.scanned)
	fmt.Printf("  flagged:     %d\n", rep.flagged)
	fmt.Printf("  updated:     %d\n", rep.updated)
	if rep.contextRead > 0 || rep.contextMissing > 0 {
		fmt.Printf("  re-read:     %d judged with context, %d unrecoverable\n", rep.contextRead, rep.contextMissing)
	}
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
