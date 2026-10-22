// Command sweep_builtin_presets is an offline operator tool that re-evaluates
// stored risk findings against current policy filters. ClickHouse candidate
// mode applies disabled rules and the built-in preset false-positive catalog.
// Postgres candidate mode preserves the historical preset-only behavior.
//
// It is meant to be run rarely and by hand, against production databases
// reached through tunnels:
//
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	GRAM_CLICKHOUSE_URL=clickhouse://USER:PASS@HOST:9440/gram?secure=true \
//	  go run ./server/cmd/tools/sweep_builtin_presets \
//	  -candidates clickhouse -org <ORG_ID> -project <PROJECT_ID> \
//	  -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
//	  -dry-run=false
//
// Safety properties:
//   - -dry-run defaults to true: a plain run only reports what it would mark.
//   - -candidates defaults to postgres, preserving the historical scan and its
//     unconditional preset matching. clickhouse mode uses each policy's current
//     disabled rules and built-in preset toggle.
//   - ClickHouse candidates are latest live, non-shadow copies, keyset-paged by
//     (created_at, id). The reported id resumes through -cursor.
//   - Marks are appended to ClickHouse first, then mirrored to Postgres. Every
//     write is scoped by organization_id + project_id + id and skips findings
//     already dismissed, so re-runs and resumes are idempotent.
//   - Marks are reversible through the risk results restore API, which
//     appends ClickHouse reversal copies and clears the Postgres mirror. A
//     Postgres UPDATE alone does not undo a sweep.
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
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/internal/clickhouseconn"
	"github.com/speakeasy-api/gram/server/cmd/tools/internal/falsepositivesweep"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

const (
	defaultBatchSize = 5000
	maxBatchSize     = 1_000_000

	candidateSourcePostgres   candidateSource = "postgres"
	candidateSourceClickHouse candidateSource = "clickhouse"

	filterBuiltinPreset = "builtin_preset"
	filterDisabledRule  = "disabled_rule"
)

type candidateSource string

type config struct {
	dbURL          string
	clickhouseURL  string
	assetsBackend  string
	assetsURI      string
	orgID          string
	projectID      uuid.UUID
	policyID       uuid.NullUUID
	from           time.Time
	to             time.Time
	cursor         uuid.UUID
	cursorProvided bool
	batchSize      int
	dryRun         bool
	candidates     candidateSource
}

type report struct {
	scanned           int64
	flagged           int64
	updated           int64
	missingPolicy     int64
	disabledPolicy    int64
	unreconstructable int64
	redactionMismatch int64
	bySource          map[string]int64
	byFilter          map[string]int64
	byReason          map[string]int64
	lastCursor        uuid.UUID
}

type policySweepFilters struct {
	disabledRules         risk_analysis.DisabledRuleSet
	builtinPresetsEnabled bool
	found                 bool
	enabled               bool
}

func emptyPolicySweepFilters() policySweepFilters {
	return policySweepFilters{
		disabledRules:         risk_analysis.NewDisabledRuleSet(nil),
		builtinPresetsEnabled: false,
		found:                 false,
		enabled:               false,
	}
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

	chConn, err := clickhouseconn.Open(ctx, cfg.clickhouseURL)
	if err != nil {
		log.Printf("connect ClickHouse: %v", err)
		return 1
	}
	defer func() { _ = chConn.Close() }()

	var assetStorage blobio.Reader
	closeAssets := func() error { return nil }
	if cfg.candidates == candidateSourceClickHouse {
		assetStorage, closeAssets, err = openAssetStorage(ctx, slog.Default(), cfg.assetsBackend, cfg.assetsURI)
		if err != nil {
			log.Printf("connect asset storage: %v", err)
			return 1
		}
	}
	defer func() {
		if err := closeAssets(); err != nil {
			log.Printf("close asset storage: %v", err)
		}
	}()

	rep, runErr := sweep(ctx, pool, chrepo.New(chConn), cfg, lib, assetStorage)
	printReport(cfg, rep, lib)

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Printf("sweep failed: %v", runErr)
		return 1
	}
	return 0
}

func parseFlags() (config, error) {
	var (
		dbURL         = flag.String("db", os.Getenv("GRAM_DATABASE_URL"), "Postgres connection string (defaults to $GRAM_DATABASE_URL)")
		clickhouseURL = flag.String("clickhouse", os.Getenv("GRAM_CLICKHOUSE_URL"), "ClickHouse connection string (defaults to $GRAM_CLICKHOUSE_URL)")
		assetsBackend = flag.String("assets-backend", os.Getenv("GRAM_ASSETS_BACKEND"), "optional asset backend for content-part reconstruction: fs or gcs (defaults to $GRAM_ASSETS_BACKEND)")
		assetsURI     = flag.String("assets-uri", os.Getenv("GRAM_ASSETS_URI"), "optional asset storage location for content-part reconstruction (defaults to $GRAM_ASSETS_URI)")
		orgID         = flag.String("org", "", "organization_id to scope the sweep (required)")
		projectID     = flag.String("project", "", "project_id (uuid) to scope the sweep (required)")
		policyID      = flag.String("policy", "", "optional risk_policy_id (uuid) to scope to one policy; all policies if empty")
		fromStr       = flag.String("from", "", "lower time bound, RFC3339 (required); findings created at/after this are scanned")
		toStr         = flag.String("to", "", "upper time bound, RFC3339 (required); findings created before this are scanned")
		cursorStr     = flag.String("cursor", "", "optional finding id to resume after (exclusive)")
		batchSize     = flag.Int("batch-size", defaultBatchSize, "rows fetched and updated per page")
		dryRun        = flag.Bool("dry-run", true, "when true (default) only report; pass -dry-run=false to write")
		candidates    = flag.String("candidates", string(candidateSourcePostgres), "candidate source: postgres or clickhouse")
	)
	flag.Parse()

	var cfg config
	cfg.dbURL = *dbURL
	cfg.clickhouseURL = *clickhouseURL
	cfg.assetsBackend = *assetsBackend
	cfg.assetsURI = *assetsURI
	cfg.orgID = *orgID
	cfg.dryRun = *dryRun
	cfg.candidates = candidateSource(*candidates)
	switch cfg.candidates {
	case candidateSourcePostgres, candidateSourceClickHouse:
	default:
		return cfg, errors.New("-candidates must be postgres or clickhouse")
	}

	if cfg.dbURL == "" {
		return cfg, errors.New("missing -db / $GRAM_DATABASE_URL")
	}
	if cfg.clickhouseURL == "" {
		return cfg, errors.New("missing -clickhouse / $GRAM_CLICKHOUSE_URL")
	}
	if cfg.candidates == candidateSourceClickHouse {
		if (cfg.assetsBackend == "") != (cfg.assetsURI == "") {
			return cfg, errors.New("-assets-backend and -assets-uri must be provided together")
		}
		if cfg.assetsBackend != "" && cfg.assetsBackend != "fs" && cfg.assetsBackend != "gcs" {
			return cfg, errors.New("-assets-backend must be fs or gcs")
		}
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
	cfg.batchSize = *batchSize

	// The keyset cursor is exclusive (id > cursor). Start just below the lower
	// time bound so the first real finding at -from is included; -cursor lets a
	// resumed run pick up where an interrupted one stopped.
	if *cursorStr != "" {
		cfg.cursor, err = uuid.Parse(*cursorStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
		cfg.cursorProvided = true
	} else {
		cfg.cursor = uuidv7.LowerBound(cfg.from)
	}

	return cfg, nil
}

func openAssetStorage(ctx context.Context, logger *slog.Logger, backend, uri string) (blobio.Reader, func() error, error) {
	closeStorage := func() error { return nil }
	switch backend {
	case "":
		return nil, closeStorage, nil
	case "fs":
		root, err := os.OpenRoot(filepath.Clean(uri))
		if err != nil {
			return nil, closeStorage, fmt.Errorf("open fs assets root: %w", err)
		}
		return assets.NewFSBlobStore(logger, root), root.Close, nil
	case "gcs":
		store, err := assets.NewGCSBlobStore(ctx, logger, uri)
		if err != nil {
			return nil, closeStorage, fmt.Errorf("create gcs blob store: %w", err)
		}
		return store, closeStorage, nil
	default:
		return nil, closeStorage, fmt.Errorf("invalid assets backend %q", backend)
	}
}

// selectPage walks risk_results in id order. It only fetches rows whose rule_id
// is scoped by the preset catalog — either an exact id (Library.RuleIDs) or a
// "prefix*" family exposed as a SQL LIKE pattern (Library.RuleIDGlobs, see
// likeGlobs) — and that are still active (found, not excluded, not already
// swept), within the id/time window. source and match are fetched because the
// catalog classifies on all three axes.
const selectPage = `
SELECT id, source, rule_id, match
FROM risk_results
WHERE organization_id = $1
  AND project_id = $2
  AND ($3::uuid IS NULL OR risk_policy_id = $3)
  AND found IS TRUE
  AND excluded_at IS NULL
  AND false_positive_at IS NULL
  AND (rule_id = ANY($4::text[]) OR rule_id LIKE ANY($5::text[]))
  AND id > $6
  AND id < $7
ORDER BY id
LIMIT $8
`

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

func sweep(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, cfg config, lib *presetlib.Library, assetStorage blobio.Reader) (report, error) {
	rep := report{
		scanned:           0,
		flagged:           0,
		updated:           0,
		missingPolicy:     0,
		disabledPolicy:    0,
		unreconstructable: 0,
		redactionMismatch: 0,
		bySource:          map[string]int64{},
		byFilter:          map[string]int64{},
		byReason:          map[string]int64{},
		lastCursor:        cfg.cursor,
	}
	if cfg.candidates == candidateSourceClickHouse && !cfg.cursorProvided {
		rep.lastCursor = uuid.Nil
	}

	switch cfg.candidates {
	case candidateSourcePostgres:
		return sweepPostgres(ctx, pool, ch, cfg, lib, rep)
	case candidateSourceClickHouse:
		return sweepClickHouse(ctx, pool, ch, cfg, lib, rep, assetStorage)
	default:
		return rep, fmt.Errorf("unsupported candidate source %q", cfg.candidates)
	}
}

func sweepPostgres(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, cfg config, lib *presetlib.Library, rep report) (report, error) {
	ruleIDs := lib.RuleIDs()
	globs := likeGlobs(lib.RuleIDGlobs())
	provenance := "preset:" + lib.Version() + " "
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
			cfg.orgID, cfg.projectID, policyArg, ruleIDs, globs, cursor, upper, cfg.batchSize)
		if err != nil {
			return rep, fmt.Errorf("select Postgres page after %s: %w", cursor, err)
		}

		var (
			ids     []uuid.UUID
			reasons []string
			n       int
		)
		for rows.Next() {
			var (
				id     uuid.UUID
				source string
				ruleID string
				match  *string
			)
			if err := rows.Scan(&id, &source, &ruleID, &match); err != nil {
				rows.Close()
				return rep, fmt.Errorf("scan Postgres row: %w", err)
			}
			n++
			cursor = id
			if match == nil {
				continue
			}
			reason := lib.Reason(presetlib.Match{Source: source, RuleID: ruleID, Value: *match})
			if reason == "" {
				continue
			}
			ids = append(ids, id)
			reasons = append(reasons, provenance+reason)
			rep.bySource[source]++
			rep.byFilter[filterBuiltinPreset]++
			rep.byReason[reason]++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return rep, fmt.Errorf("iterate Postgres page: %w", err)
		}
		rows.Close()

		rep.scanned += int64(n)
		rep.flagged += int64(len(ids))
		if err := applyBatch(ctx, pool, ch, cfg, &rep, ids, reasons, cursor); err != nil {
			return rep, err
		}
		rep.lastCursor = cursor
		logProgress(cfg, rep)

		if n < cfg.batchSize {
			return rep, nil
		}
	}
}

func sweepClickHouse(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, cfg config, lib *presetlib.Library, rep report, assetStorage blobio.Reader) (report, error) {
	cursorTime := cfg.from
	cursorID := uuid.Nil
	if cfg.cursorProvided {
		resolved, err := ch.GetPolicyFilterSweepCursorTime(ctx, cfg.orgID, cfg.projectID.String(), cfg.cursor)
		if err != nil {
			return rep, fmt.Errorf("resolve ClickHouse resume cursor: %w", err)
		}
		cursorTime = resolved
		cursorID = cfg.cursor
	}

	policyID := ""
	if cfg.policyID.Valid {
		policyID = cfg.policyID.UUID.String()
	}
	policyCache := map[uuid.UUID]policySweepFilters{}
	reveal := risk.NewRevealMatcher(slog.Default(), riskrepo.New(pool), assetStorage)
	provenance := "preset:" + lib.Version() + " "

	for {
		if err := ctx.Err(); err != nil {
			return rep, fmt.Errorf("sweep interrupted at %s: %w", cursorID, err)
		}

		candidates, err := ch.ListPolicyFilterSweepCandidates(ctx, chrepo.ListPolicyFilterSweepCandidatesParams{
			OrganizationID: cfg.orgID,
			ProjectID:      cfg.projectID.String(),
			PolicyID:       policyID,
			From:           cfg.from,
			To:             cfg.to,
			CursorTime:     cursorTime,
			CursorID:       cursorID,
			Limit:          cfg.batchSize,
		})
		if err != nil {
			return rep, fmt.Errorf("select ClickHouse page after %s: %w", cursorID, err)
		}

		ids := make([]uuid.UUID, 0, len(candidates))
		reasons := make([]string, 0, len(candidates))
		anchors := make(map[string]risk.RevealAnchor)
		missingPolicies := make(map[uuid.UUID]struct{})
		for i := range candidates {
			candidate := &candidates[i]
			cursorTime = candidate.CreatedAt
			cursorID = candidate.ID

			filters, err := loadPolicySweepFilters(ctx, pool, policyCache, missingPolicies, cfg.projectID, candidate.RiskPolicyID)
			if err != nil {
				return rep, err
			}
			if !filters.found {
				rep.missingPolicy++
				continue
			}
			if !filters.enabled {
				rep.disabledPolicy++
				continue
			}

			var (
				filter string
				detail string
			)
			switch {
			case filters.disabledRules.Contains(candidate.RuleID):
				filter = filterDisabledRule
				detail = filterDisabledRule + ":" + candidate.RuleID
			case filters.builtinPresetsEnabled:
				match, ok := reconstructCandidateMatch(ctx, reveal, cfg.projectID, candidate, anchors)
				if !ok {
					rep.unreconstructable++
					continue
				}
				if maskdisplay.Display(candidate.Source, candidate.RuleID, match) != candidate.MatchRedacted {
					rep.redactionMismatch++
					continue
				}
				if !risk_analysis.IsBuiltinFalsePositive(lib, candidate.Source, candidate.RuleID, match) {
					continue
				}
				reason := lib.Reason(presetlib.Match{Source: candidate.Source, RuleID: candidate.RuleID, Value: match})
				filter = filterBuiltinPreset
				detail = provenance + reason
				rep.byReason[reason]++
			default:
				continue
			}

			ids = append(ids, candidate.ID)
			reasons = append(reasons, detail)
			rep.bySource[candidate.Source]++
			rep.byFilter[filter]++
		}

		rep.scanned += int64(len(candidates))
		rep.flagged += int64(len(ids))
		if err := applyBatch(ctx, pool, ch, cfg, &rep, ids, reasons, cursorID); err != nil {
			return rep, err
		}
		rep.lastCursor = cursorID
		logProgress(cfg, rep)

		if len(candidates) < cfg.batchSize {
			return rep, nil
		}
	}
}

func loadPolicySweepFilters(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache map[uuid.UUID]policySweepFilters,
	missing map[uuid.UUID]struct{},
	projectID uuid.UUID,
	rawPolicyID string,
) (policySweepFilters, error) {
	policyID, err := uuid.Parse(rawPolicyID)
	if err != nil {
		return emptyPolicySweepFilters(), nil
	}
	if filters, ok := cache[policyID]; ok {
		return filters, nil
	}
	if _, ok := missing[policyID]; ok {
		return emptyPolicySweepFilters(), nil
	}

	policy, err := riskrepo.New(pool).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		missing[policyID] = struct{}{}
		return emptyPolicySweepFilters(), nil
	}
	if err != nil {
		return policySweepFilters{}, fmt.Errorf("get risk policy %s: %w", policyID, err)
	}

	filters := policySweepFilters{
		disabledRules:         risk_analysis.NewDisabledRuleSet(policy.DisabledRules),
		builtinPresetsEnabled: risk_analysis.BuiltinPresetsEnabledFromConfig(policy.AnalyzerConfig),
		found:                 true,
		enabled:               policy.Enabled,
	}
	cache[policyID] = filters
	return filters, nil
}

func reconstructCandidateMatch(ctx context.Context, reveal *risk.RevealMatcher, projectID uuid.UUID, candidate *chrepo.RiskFindingUnmaskRow, anchors map[string]risk.RevealAnchor) (string, bool) {
	anchorKey := candidate.ChatMessageID + "\x00" + candidate.ContentPartID
	anchor, ok := anchors[anchorKey]
	if !ok {
		anchor = reveal.LoadAnchor(ctx, projectID, candidate)
		reveal.HydratePartContent(ctx, &anchor)
		anchors[anchorKey] = anchor
	}
	chatID, attributed := risk.ResolveChatID(candidate, anchor)
	if !attributed {
		return "", false
	}
	return risk.MatchingReconstruction(candidate.MatchLen, reveal.Candidates(ctx, chatID, candidate, anchor))
}

func applyBatch(ctx context.Context, pool *pgxpool.Pool, ch *chrepo.Queries, cfg config, rep *report, ids []uuid.UUID, reasons []string, cursor uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	changed, err := falsepositivesweep.Apply(ctx, pool, ch, cfg.orgID, cfg.projectID, ids, reasons, cfg.dryRun)
	if !cfg.dryRun {
		rep.updated += changed
	}
	if err != nil {
		return fmt.Errorf("apply batch ending at %s: %w", cursor, err)
	}
	return nil
}

func logProgress(cfg config, rep report) {
	log.Printf("candidates=%s scanned=%d flagged=%d updated=%d missing_policy=%d disabled_policy=%d unreconstructable=%d redaction_mismatch=%d cursor=%s",
		cfg.candidates, rep.scanned, rep.flagged, rep.updated, rep.missingPolicy, rep.disabledPolicy, rep.unreconstructable, rep.redactionMismatch, rep.lastCursor)
}

func printReport(cfg config, rep report, lib *presetlib.Library) {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("policy filter sweep summary")
	fmt.Printf("  catalog:        preset:%s\n", lib.Version())
	fmt.Printf("  mode:           %s\n", mode)
	fmt.Printf("  candidates:     %s\n", cfg.candidates)
	fmt.Printf("  org:            %s\n", cfg.orgID)
	fmt.Printf("  project:        %s\n", cfg.projectID)
	if cfg.policyID.Valid {
		fmt.Printf("  policy:         %s\n", cfg.policyID.UUID)
	} else {
		fmt.Printf("  policy:         (all)\n")
	}
	fmt.Printf("  window:              %s .. %s\n", cfg.from.Format(time.RFC3339), cfg.to.Format(time.RFC3339))
	fmt.Printf("  scanned:             %d\n", rep.scanned)
	fmt.Printf("  flagged:             %d\n", rep.flagged)
	fmt.Printf("  updated:             %d\n", rep.updated)
	fmt.Printf("  missing policy:      %d\n", rep.missingPolicy)
	fmt.Printf("  disabled policy:     %d\n", rep.disabledPolicy)
	fmt.Printf("  unreconstructable:   %d\n", rep.unreconstructable)
	fmt.Printf("  redaction mismatch:  %d\n", rep.redactionMismatch)
	fmt.Printf("  last cursor:         %s\n", rep.lastCursor)

	printCounts("  by source:", rep.bySource)
	printCounts("  by filter:", rep.byFilter)

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

func printCounts(heading string, counts map[string]int64) {
	if len(counts) == 0 {
		return
	}
	fmt.Println(heading)
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		fmt.Printf("    %6d  %s\n", counts[key], key)
	}
}
