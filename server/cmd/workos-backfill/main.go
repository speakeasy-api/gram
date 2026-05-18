package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/term"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const sampleSize = 5
const updateDetailLimit = 20
const changeSummarySampleLimit = 3
const defaultStatementTimeout = 30 * time.Minute

type phase string

const (
	phasePreflight     phase = "preflight"
	phaseGlobalRoles   phase = "global-roles"
	phaseOrganizations phase = "organizations"
	phaseValidate      phase = "validate"
	phaseAll           phase = "all"
)

type environment string

const (
	envLocal environment = "local"
	envDev   environment = "dev"
	envProd  environment = "prod"
)

type options struct {
	phase            phase
	environment      environment
	databaseURL      string
	cloudSQLProxy    bool
	cloudSQLPort     int
	cloudSQLDBName   string
	workosAPIKey     string
	workosEndpoint   string
	workosOrgIDs     []string
	limit            int
	pageSize         int
	pageOffset       int
	statementTimeout time.Duration
	dryRun           bool
	autoApprove      bool
	pauseAfterEach   bool
	confirmProd      string
	breakpointBefore bool
}

type orgExpectation struct {
	workosOrgID       string
	gramOrgID         string
	name              string
	skipped           bool
	roles             []workos.Role
	users             map[string]workos.User
	members           []workos.Member
	orgChanges        changeCounts
	roleChanges       changeCounts
	userChanges       changeCounts
	membershipChanges changeCounts
	assignmentChanges changeCounts
	changeDetails     []changeDetail
}

type changeDetail struct {
	Entity string
	ID     string
	Action string
	Fields []fieldChange
}

type fieldChange struct {
	Name   string
	Before string
	After  string
}

type changeSummaryGroup struct {
	Entity  string
	Action  string
	Risk    string
	Fields  []string
	Count   int
	Samples []changeDetail
}

type report struct {
	scanned            int
	skipped            int
	skippedNoop        int
	written            int
	validated          int
	failed             int
	validationFailures int
	organizationRows   changeCounts
	roleRows           changeCounts
	userRows           changeCounts
	membershipRows     changeCounts
	assignmentRows     changeCounts
}

type changeCounts struct {
	Create    int
	Update    int
	Noop      int
	Delete    int
	StaleSkip int
}

func (c changeCounts) Add(other changeCounts) changeCounts {
	return changeCounts{
		Create:    c.Create + other.Create,
		Update:    c.Update + other.Update,
		Noop:      c.Noop + other.Noop,
		Delete:    c.Delete + other.Delete,
		StaleSkip: c.StaleSkip + other.StaleSkip,
	}
}

func (c changeCounts) Mutating() int {
	return c.Create + c.Update + c.Delete
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func main() {
	ctx := context.Background()
	opts := parseFlags()
	if err := run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "workos-backfill: %v%s\n", err, cloudSQLRunHint(err, opts))
		os.Exit(1)
	}
}

func parseFlags() options {
	opts := options{
		phase:            phasePreflight,
		environment:      envLocal,
		databaseURL:      strings.TrimSpace(os.Getenv("GRAM_DATABASE_URL")),
		cloudSQLProxy:    false,
		cloudSQLPort:     0,
		cloudSQLDBName:   "gram",
		workosAPIKey:     strings.TrimSpace(firstNonEmpty(os.Getenv("WORKOS_API_KEY"), os.Getenv("WORK_OS_SECRET_KEY"))),
		workosEndpoint:   strings.TrimSpace(os.Getenv("WORKOS_API_URL")),
		workosOrgIDs:     nil,
		limit:            0,
		pageSize:         0,
		pageOffset:       0,
		statementTimeout: defaultStatementTimeout,
		dryRun:           true,
		autoApprove:      false,
		pauseAfterEach:   false,
		confirmProd:      "",
		breakpointBefore: false,
	}

	var rawPhase string
	var rawEnv string
	var orgIDs stringList
	flag.StringVar(&rawPhase, "phase", string(opts.phase), "phase to run: preflight, global-roles, organizations, validate, all")
	flag.StringVar(&rawEnv, "environment", string(opts.environment), "target environment: local, dev, prod")
	flag.StringVar(&opts.databaseURL, "database-url", opts.databaseURL, "Postgres connection URL (defaults to GRAM_DATABASE_URL)")
	flag.BoolVar(&opts.cloudSQLProxy, "cloudsql-proxy", opts.cloudSQLProxy, "start a local Cloud SQL proxy and connect through it")
	flag.IntVar(&opts.cloudSQLPort, "cloudsql-port", opts.cloudSQLPort, "local Cloud SQL proxy port (defaults to a free port)")
	flag.StringVar(&opts.cloudSQLDBName, "cloudsql-db-name", opts.cloudSQLDBName, "Cloud SQL database name")
	flag.StringVar(&opts.workosAPIKey, "workos-api-key", opts.workosAPIKey, "WorkOS API key (defaults to WORKOS_API_KEY or WORK_OS_SECRET_KEY)")
	flag.StringVar(&opts.workosEndpoint, "workos-endpoint", opts.workosEndpoint, "WorkOS API endpoint override (defaults to WORKOS_API_URL)")
	flag.Var(&orgIDs, "workos-org-id", "WorkOS organization id to process; repeat or comma-separate")
	flag.IntVar(&opts.limit, "limit", opts.limit, "maximum organizations to inspect or backfill")
	flag.IntVar(&opts.pageSize, "page-size", opts.pageSize, "number of organizations to inspect or backfill after page offset (0 means all remaining)")
	flag.IntVar(&opts.pageOffset, "page-offset", opts.pageOffset, "number of organizations to skip after deterministic sorting")
	flag.DurationVar(&opts.statementTimeout, "statement-timeout", opts.statementTimeout, "Postgres statement_timeout for each DB connection")
	flag.BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "inspect and validate without DB writes")
	flag.BoolVar(&opts.autoApprove, "auto-approve", opts.autoApprove, "skip non-prod write confirmations")
	flag.BoolVar(&opts.pauseAfterEach, "pause-after-each", opts.pauseAfterEach, "wait for Enter after each organization")
	flag.BoolVar(&opts.breakpointBefore, "breakpoint-before-write", opts.breakpointBefore, "wait for Enter after preflight before writes")
	flag.StringVar(&opts.confirmProd, "confirm-prod", opts.confirmProd, "must be set to prod for non-interactive prod access")
	flag.Parse()

	opts.phase = phase(rawPhase)
	opts.environment = environment(rawEnv)
	opts.workosOrgIDs = orgIDs
	must(validateOptions(opts))
	return opts
}

func validateOptions(opts options) error {
	switch opts.phase {
	case phasePreflight, phaseGlobalRoles, phaseOrganizations, phaseValidate, phaseAll:
	default:
		return fmt.Errorf("invalid phase %q", opts.phase)
	}

	switch opts.environment {
	case envLocal, envDev, envProd:
	default:
		return fmt.Errorf("invalid environment %q", opts.environment)
	}

	if opts.cloudSQLProxy && opts.environment == envLocal {
		return errors.New("--cloudsql-proxy requires --environment=dev or --environment=prod")
	}
	if opts.cloudSQLPort < 0 || opts.cloudSQLPort > 65535 {
		return errors.New("--cloudsql-port must be between 0 and 65535")
	}
	if strings.TrimSpace(opts.cloudSQLDBName) == "" {
		return errors.New("--cloudsql-db-name must be non-empty")
	}
	if opts.databaseURL == "" && !opts.cloudSQLProxy {
		return errors.New("--database-url or GRAM_DATABASE_URL is required")
	}
	if opts.workosAPIKey == "" {
		return errors.New("--workos-api-key, WORKOS_API_KEY, or WORK_OS_SECRET_KEY is required")
	}
	if opts.limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if opts.pageSize < 0 {
		return errors.New("--page-size must be non-negative")
	}
	if opts.pageOffset < 0 {
		return errors.New("--page-offset must be non-negative")
	}
	if opts.statementTimeout <= 0 {
		return errors.New("--statement-timeout must be positive")
	}
	if opts.workosEndpoint == "" {
		if opts.environment == envProd {
			if strings.HasPrefix(opts.workosAPIKey, "sk_test_") || !strings.HasPrefix(opts.workosAPIKey, "sk_") {
				return errors.New("prod WorkOS key must be live and start with sk_, not sk_test_")
			}
		} else if !strings.HasPrefix(opts.workosAPIKey, "sk_test_") {
			return fmt.Errorf("%s WorkOS key must start with sk_test_ when using the real WorkOS endpoint", opts.environment)
		}
	}

	return nil
}

func run(ctx context.Context, opts options) error {
	logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
		RawLevel:    "info",
		Pretty:      true,
		DataDogAttr: false,
	}))

	if opts.environment == envProd {
		if err := confirmProdAccess(opts); err != nil {
			return err
		}
	}

	readOnly := opts.dryRun || opts.phase == phasePreflight || opts.phase == phaseValidate
	databaseURL := opts.databaseURL
	var cleanupCloudSQLProxy func()
	if opts.cloudSQLProxy {
		var err error
		databaseURL, cleanupCloudSQLProxy, err = startCloudSQLProxy(ctx, opts, readOnly)
		if err != nil {
			return err
		}
		defer cleanupCloudSQLProxy()
	}

	db, err := connectDB(ctx, databaseURL, readOnly, opts.statementTimeout)
	if err != nil {
		if opts.cloudSQLProxy {
			return fmt.Errorf("%w%s", err, cloudSQLProxyHint())
		}
		return err
	}
	defer db.Close()

	workosClient, err := newWorkOSClient(opts)
	if err != nil {
		return err
	}

	fmt.Printf("WorkOS backfill phase=%s environment=%s dry_run=%t read_only_db=%t\n", opts.phase, opts.environment, opts.dryRun, readOnly)
	fmt.Printf("Database statement_timeout: %s\n", opts.statementTimeout)
	if opts.pageOffset > 0 || opts.pageSize > 0 {
		fmt.Printf("Organization page: offset=%d size=%d\n", opts.pageOffset, opts.pageSize)
	}
	if opts.cloudSQLProxy {
		fmt.Println("Database connection: local Cloud SQL proxy")
	}
	if opts.workosEndpoint != "" {
		fmt.Printf("WorkOS endpoint override: %s\n", opts.workosEndpoint)
	}

	var success = true
	if opts.phase == phaseGlobalRoles || opts.phase == phaseValidate || opts.phase == phaseAll || opts.phase == phasePreflight {
		globalRoles, err := workosClient.ListGlobalRoles(ctx)
		if err != nil {
			return fmt.Errorf("list WorkOS global roles: %w", err)
		}
		globalRoleChanges, err := classifyGlobalRoleChanges(ctx, db, globalRoles)
		if err != nil {
			return err
		}
		globalRoleDetails, err := collectGlobalRoleChangeDetails(ctx, db, globalRoles)
		if err != nil {
			return err
		}
		printGlobalRolePlan(globalRoles, globalRoleChanges, globalRoleDetails)

		if opts.phase == phaseGlobalRoles || opts.phase == phaseAll {
			if opts.dryRun {
				fmt.Println("Dry-run enabled: global role backfill writes skipped.")
			} else if globalRoleChanges.Mutating() == 0 {
				fmt.Println("Global role backfill skipped: no planned row changes.")
			} else {
				if err := confirmWrite(opts, fmt.Sprintf("global role changes: create=%d update=%d delete=%d noop=%d stale_skip=%d",
					globalRoleChanges.Create,
					globalRoleChanges.Update,
					globalRoleChanges.Delete,
					globalRoleChanges.Noop,
					globalRoleChanges.StaleSkip,
				)); err != nil {
					return err
				}
				if opts.breakpointBefore {
					waitForEnter("Breakpoint before global role writes. Press Enter to continue.")
				}
				if err := NewBackfillWorkOSGlobalRoles(logger, db, workosClient).Do(ctx); err != nil {
					return fmt.Errorf("backfill WorkOS global roles: %w", err)
				}
			}
		}

		if shouldValidate(opts) && (opts.phase == phaseGlobalRoles || opts.phase == phaseValidate || opts.phase == phaseAll) {
			rep := validateGlobalRoles(ctx, db, globalRoles)
			printReport("Global role validation complete.", rep)
			success = rep.validationFailures == 0 && rep.failed == 0 && success
		} else if opts.dryRun && (opts.phase == phaseGlobalRoles || opts.phase == phaseAll) {
			fmt.Println("Dry-run enabled: global role validation skipped because writes were not performed.")
		}
	}

	if opts.phase == phasePreflight || opts.phase == phaseOrganizations || opts.phase == phaseValidate || opts.phase == phaseAll {
		orgs, err := buildOrganizationPlan(ctx, db, workosClient, opts)
		if err != nil {
			return err
		}
		printOrganizationPlan(orgs)

		if opts.phase == phasePreflight {
			return nil
		}
		if opts.phase == phaseOrganizations || opts.phase == phaseAll {
			if opts.dryRun {
				fmt.Println("Dry-run enabled: organization backfill writes skipped.")
			} else {
				if plannedOrganizationMutations(orgs) > 0 {
					if err := confirmWrite(opts, organizationSummary(orgs)); err != nil {
						return err
					}
					if opts.breakpointBefore {
						waitForEnter("Breakpoint before organization writes. Press Enter to continue.")
					}
				}
				rep := runOrganizationBackfill(ctx, logger, db, workosClient, opts, orgs)
				printReport("Organization backfill complete.", rep)
				success = rep.failed == 0 && rep.validationFailures == 0 && success
			}
		}
		if shouldValidate(opts) && (opts.phase == phaseValidate || opts.phase == phaseAll) {
			rep := validateOrganizations(ctx, db, orgs)
			printReport("Organization validation complete.", rep)
			success = rep.validationFailures == 0 && rep.failed == 0 && success
		} else if opts.dryRun && (opts.phase == phaseOrganizations || opts.phase == phaseAll) {
			fmt.Println("Dry-run enabled: organization validation skipped because writes were not performed.")
		}
	}

	if !success {
		return errors.New("backfill completed with failures")
	}
	return nil
}

func shouldValidate(opts options) bool {
	return opts.phase == phaseValidate || !opts.dryRun
}

func connectDB(ctx context.Context, databaseURL string, readOnly bool, statementTimeout time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	statementTimeoutMs := max(1, statementTimeout.Milliseconds())
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET lock_timeout = '5s'"); err != nil {
			return fmt.Errorf("set lock_timeout: %w", err)
		}
		if _, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", statementTimeoutMs)); err != nil {
			return fmt.Errorf("set statement_timeout: %w", err)
		}
		if readOnly {
			if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
				return fmt.Errorf("set default_transaction_read_only: %w", err)
			}
		}
		return nil
	}

	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func retryTransientDBDisconnect(ctx context.Context, label string, fn func() error) error {
	const attempts = 3
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isTransientDBDisconnect(err) || attempt == attempts {
			return err
		}

		delay := time.Duration(attempt) * 500 * time.Millisecond
		fmt.Fprintf(os.Stderr, "WARN  transient database disconnect during %s; retrying in %s: %v\n", label, delay, err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("%s: %w", label, ctx.Err())
		case <-timer.C:
		}
	}
	return err
}

func isTransientDBDisconnect(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57P01", "57P02", "57P03", "08000", "08003", "08006", "08007", "08P01":
			return true
		default:
			return false
		}
	}
	return strings.Contains(err.Error(), "SQLSTATE 57P01") ||
		strings.Contains(err.Error(), "terminating connection due to administrator command") ||
		strings.Contains(err.Error(), "conn closed")
}

func newWorkOSClient(opts options) (*workos.Client, error) {
	tracerProvider := noop.NewTracerProvider()
	policy := guardian.NewDefaultPolicy(tracerProvider)
	if opts.workosEndpoint != "" {
		unsafePolicy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
		if err != nil {
			return nil, fmt.Errorf("create unsafe guardian policy: %w", err)
		}
		policy = unsafePolicy
	}

	return workos.NewClient(policy, opts.workosAPIKey, workos.ClientOpts{
		Endpoint:   opts.workosEndpoint,
		HTTPClient: nil,
	}), nil
}

func buildOrganizationPlan(ctx context.Context, db *pgxpool.Pool, workosClient *workos.Client, opts options) ([]orgExpectation, error) {
	workosOrgs, err := selectedOrganizations(ctx, workosClient, opts)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Planning organization backfill for %d WorkOS organizations\n", len(workosOrgs))

	out := make([]orgExpectation, 0, len(workosOrgs))
	for i, org := range workosOrgs {
		fmt.Printf("[%d/%d] plan %s name=%q\n", i+1, len(workosOrgs), org.ID, org.Name)
		expectation, err := planOrganizationWithRetry(ctx, db, workosClient, org)
		if err != nil {
			return nil, err
		}
		out = append(out, expectation)
	}

	return out, nil
}

func planOrganizationWithRetry(ctx context.Context, db *pgxpool.Pool, workosClient *workos.Client, org workos.Organization) (orgExpectation, error) {
	var expectation orgExpectation
	err := retryTransientDBDisconnect(ctx, fmt.Sprintf("plan organization %s", org.ID), func() error {
		next, err := planOrganization(ctx, db, workosClient, org)
		if err != nil {
			return err
		}
		expectation = next
		return nil
	})
	return expectation, err
}

func planOrganization(ctx context.Context, db *pgxpool.Pool, workosClient *workos.Client, org workos.Organization) (orgExpectation, error) {
	var zero orgExpectation
	gramOrgID, skipped, err := expectedGramOrgID(ctx, db, org)
	if err != nil {
		return zero, err
	}
	roles, err := workosClient.ListRoles(ctx, org.ID)
	if err != nil {
		return zero, fmt.Errorf("list roles for %s: %w", org.ID, err)
	}
	users, err := workosClient.ListOrgUsers(ctx, org.ID)
	if err != nil {
		return zero, fmt.Errorf("list users for %s: %w", org.ID, err)
	}
	members, err := workosClient.ListOrgMemberships(ctx, org.ID)
	if err != nil {
		return zero, fmt.Errorf("list memberships for %s: %w", org.ID, err)
	}

	orgChanges, err := classifyOrganizationMetadataChange(ctx, db, org, gramOrgID, skipped)
	if err != nil {
		return zero, err
	}
	roleChanges, err := classifyOrganizationRoleChanges(ctx, db, gramOrgID, skipped, roles)
	if err != nil {
		return zero, err
	}
	userChanges, err := classifyUserChanges(ctx, db, skipped, users)
	if err != nil {
		return zero, err
	}
	membershipChanges, err := classifyMembershipChanges(ctx, db, gramOrgID, skipped, users, members)
	if err != nil {
		return zero, err
	}
	assignmentChanges, err := classifyAssignmentChanges(ctx, db, gramOrgID, skipped, roles, users, members)
	if err != nil {
		return zero, err
	}
	changeDetails, err := collectOrganizationChangeDetails(ctx, db, org, gramOrgID, skipped, roles, users, members)
	if err != nil {
		return zero, err
	}

	return orgExpectation{
		workosOrgID:       org.ID,
		gramOrgID:         gramOrgID,
		name:              org.Name,
		skipped:           skipped,
		roles:             roles,
		users:             users,
		members:           members,
		orgChanges:        orgChanges,
		roleChanges:       roleChanges,
		userChanges:       userChanges,
		membershipChanges: membershipChanges,
		assignmentChanges: assignmentChanges,
		changeDetails:     changeDetails,
	}, nil
}

func selectedOrganizations(ctx context.Context, workosClient *workos.Client, opts options) ([]workos.Organization, error) {
	if len(opts.workosOrgIDs) > 0 {
		fmt.Printf("Loading %d selected WorkOS organizations\n", len(opts.workosOrgIDs))
		out := make([]workos.Organization, 0, len(opts.workosOrgIDs))
		for i, orgID := range opts.workosOrgIDs {
			fmt.Printf("[%d/%d] get WorkOS organization %s\n", i+1, len(opts.workosOrgIDs), orgID)
			org, err := workosClient.GetOrganization(ctx, orgID)
			if err != nil {
				return nil, fmt.Errorf("get WorkOS organization %s: %w", orgID, err)
			}
			out = append(out, *org)
		}
		return applyOrganizationWindow(out, opts), nil
	}

	fmt.Println("Listing WorkOS organizations")
	orgs, err := workosClient.ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list WorkOS organizations: %w", err)
	}
	fmt.Printf("Listed %d WorkOS organizations\n", len(orgs))
	sort.Slice(orgs, func(i, j int) bool { return orgs[i].ID < orgs[j].ID })
	return applyOrganizationWindow(orgs, opts), nil
}

func applyOrganizationWindow(orgs []workos.Organization, opts options) []workos.Organization {
	originalLen := len(orgs)
	if opts.pageOffset > 0 {
		if opts.pageOffset >= len(orgs) {
			fmt.Printf("Applying organization page offset: %d of %d leaves 0 organizations\n", opts.pageOffset, originalLen)
			return orgs[:0]
		}
		orgs = orgs[opts.pageOffset:]
		fmt.Printf("Applying organization page offset: skipped %d of %d\n", opts.pageOffset, originalLen)
	}
	if opts.pageSize > 0 && len(orgs) > opts.pageSize {
		fmt.Printf("Applying organization page size: %d of %d remaining\n", opts.pageSize, len(orgs))
		orgs = orgs[:opts.pageSize]
	}
	if opts.limit > 0 && len(orgs) > opts.limit {
		fmt.Printf("Applying organization limit: %d of %d\n", opts.limit, len(orgs))
		orgs = orgs[:opts.limit]
	}
	return orgs
}

func expectedGramOrgID(ctx context.Context, db *pgxpool.Pool, org workos.Organization) (string, bool, error) {
	var id string
	err := db.QueryRow(ctx, "SELECT id FROM organization_metadata WHERE workos_id = $1 LIMIT 1", org.ID).Scan(&id)
	switch {
	case err == nil:
		return id, false, nil
	case errors.Is(err, pgx.ErrNoRows):
		if org.ExternalID == "" {
			return "", true, nil
		}
		return org.ExternalID, false, nil
	default:
		return "", false, fmt.Errorf("lookup local organization by workos id %s: %w", org.ID, err)
	}
}

func classifyGlobalRoleChanges(ctx context.Context, db *pgxpool.Pool, roles []workos.Role) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	snapshotSlugs := make(map[string]struct{}, len(roles))

	for _, role := range roles {
		snapshotSlugs[role.Slug] = struct{}{}

		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return changeCounts{}, fmt.Errorf("parse global role %q updated_at: %w", role.Slug, err)
		}

		change, err := classifyRoleRow(ctx, db, "global_roles", "TRUE", nil, role, updatedAt)
		if err != nil {
			return changeCounts{}, fmt.Errorf("classify global role %q: %w", role.Slug, err)
		}
		counts = addChange(counts, change)
	}

	deleteCounts, err := classifyMissingGlobalRoleDeletes(ctx, db, snapshotSlugs)
	if err != nil {
		return changeCounts{}, err
	}
	return counts.Add(deleteCounts), nil
}

func classifyMissingGlobalRoleDeletes(ctx context.Context, db *pgxpool.Pool, snapshotSlugs map[string]struct{}) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	rows, err := db.Query(ctx, `
SELECT workos_slug, workos_updated_at, workos_last_event_id
FROM global_roles
WHERE deleted_at IS NULL`)
	if err != nil {
		return changeCounts{}, fmt.Errorf("query local global roles: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var slug string
		var updatedAt pgtype.Timestamptz
		var lastEventID pgtype.Text
		if err := rows.Scan(&slug, &updatedAt, &lastEventID); err != nil {
			return changeCounts{}, fmt.Errorf("scan local global role: %w", err)
		}
		if _, ok := snapshotSlugs[slug]; ok {
			continue
		}
		if shouldProcessEvent(textPtr(lastEventID), timePtr(updatedAt), "", now) {
			counts.Delete++
		} else {
			counts.StaleSkip++
		}
	}
	if err := rows.Err(); err != nil {
		return changeCounts{}, fmt.Errorf("iterate local global roles: %w", err)
	}
	return counts, nil
}

func classifyOrganizationMetadataChange(ctx context.Context, db *pgxpool.Pool, org workos.Organization, gramOrgID string, skipped bool) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	if skipped {
		counts.StaleSkip = 1
		return counts, nil
	}

	updatedAt, err := parseWorkOSTime(org.UpdatedAt)
	if err != nil {
		return changeCounts{}, fmt.Errorf("parse organization %q updated_at: %w", org.ID, err)
	}

	var existingID string
	var name string
	var slug string
	var workosID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var disabledAt pgtype.Timestamptz
	err = db.QueryRow(ctx, `
SELECT id, name, slug, workos_id, workos_updated_at, workos_last_event_id, disabled_at
FROM organization_metadata
WHERE workos_id = $1 OR id = $2
ORDER BY CASE WHEN workos_id = $1 THEN 0 ELSE 1 END
LIMIT 1`, org.ID, gramOrgID).Scan(&existingID, &name, &slug, &workosID, &rowUpdatedAt, &lastEventID, &disabledAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		counts.Create = 1
		return counts, nil
	case err != nil:
		return changeCounts{}, fmt.Errorf("query organization metadata %q: %w", org.ID, err)
	}

	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		counts.StaleSkip = 1
		return counts, nil
	}

	if existingID == gramOrgID &&
		name == org.Name &&
		slug != "" &&
		workosID.Valid &&
		workosID.String == org.ID &&
		!disabledAt.Valid {
		counts.Noop = 1
		return counts, nil
	}

	counts.Update = 1
	return counts, nil
}

func classifyOrganizationRoleChanges(ctx context.Context, db *pgxpool.Pool, organizationID string, skipped bool, roles []workos.Role) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	if skipped {
		return counts, nil
	}

	snapshotSlugs := map[string]struct{}{}
	for _, role := range roles {
		if role.Type != "OrganizationRole" {
			continue
		}
		snapshotSlugs[role.Slug] = struct{}{}

		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return changeCounts{}, fmt.Errorf("parse organization role %q updated_at: %w", role.Slug, err)
		}
		change, err := classifyRoleRow(ctx, db, "organization_roles", "organization_id = $1", []any{organizationID}, role, updatedAt)
		if err != nil {
			return changeCounts{}, fmt.Errorf("classify organization role %q: %w", role.Slug, err)
		}
		counts = addChange(counts, change)
	}

	deleteCounts, err := classifyMissingOrganizationRoleDeletes(ctx, db, organizationID, snapshotSlugs)
	if err != nil {
		return changeCounts{}, err
	}
	return counts.Add(deleteCounts), nil
}

func classifyMissingOrganizationRoleDeletes(ctx context.Context, db *pgxpool.Pool, organizationID string, snapshotSlugs map[string]struct{}) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	rows, err := db.Query(ctx, `
SELECT workos_slug, workos_updated_at, workos_last_event_id
FROM organization_roles
WHERE organization_id = $1
  AND deleted_at IS NULL`, organizationID)
	if err != nil {
		return changeCounts{}, fmt.Errorf("query local organization roles: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var slug string
		var updatedAt pgtype.Timestamptz
		var lastEventID pgtype.Text
		if err := rows.Scan(&slug, &updatedAt, &lastEventID); err != nil {
			return changeCounts{}, fmt.Errorf("scan local organization role: %w", err)
		}
		if _, ok := snapshotSlugs[slug]; ok {
			continue
		}
		if shouldProcessEvent(textPtr(lastEventID), timePtr(updatedAt), "", now) {
			counts.Delete++
		} else {
			counts.StaleSkip++
		}
	}
	if err := rows.Err(); err != nil {
		return changeCounts{}, fmt.Errorf("iterate local organization roles: %w", err)
	}
	return counts, nil
}

func classifyRoleRow(ctx context.Context, db *pgxpool.Pool, table, predicate string, args []any, role workos.Role, updatedAt time.Time) (string, error) {
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, role.Slug)
	slugArg := len(queryArgs)
	query := fmt.Sprintf(`
SELECT workos_name, workos_description, workos_updated_at, workos_last_event_id, workos_deleted, deleted
FROM %s
WHERE %s
  AND workos_slug = $%d
LIMIT 1`, table, predicate, slugArg) // #nosec G201 -- table and predicate are fixed call-site constants.

	var name string
	var description pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var workosDeleted bool
	var deleted bool
	err := db.QueryRow(ctx, query, queryArgs...).Scan(&name, &description, &rowUpdatedAt, &lastEventID, &workosDeleted, &deleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "create", nil
	case err != nil:
		return "", fmt.Errorf("query local role: %w", err)
	}

	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return "stale_skip", nil
	}
	if deleted || workosDeleted || name != role.Name || !pgTextEmptyEqual(description, role.Description) || !pgTimeEqual(rowUpdatedAt, updatedAt) {
		return "update", nil
	}
	return "noop", nil
}

func classifyUserChanges(ctx context.Context, db *pgxpool.Pool, skipped bool, users map[string]workos.User) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	if skipped {
		return counts, nil
	}
	for _, user := range users {
		updatedAt, err := parseWorkOSTime(user.UpdatedAt)
		if err != nil {
			counts.StaleSkip++
			continue
		}
		change, err := classifyUserRow(ctx, db, user, updatedAt)
		if err != nil {
			return changeCounts{}, err
		}
		counts = addChange(counts, change)
	}
	return counts, nil
}

func classifyUserRow(ctx context.Context, db *pgxpool.Pool, user workos.User, updatedAt time.Time) (string, error) {
	existing, found, err := findUserByWorkOSID(ctx, db, user.ID)
	if err != nil {
		return "", err
	}
	if !found {
		if user.ExternalID == "" {
			return "stale_skip", nil
		}
		existing, found, err = findUserByID(ctx, db, user.ExternalID)
		if err != nil {
			return "", err
		}
		if !found {
			return "create", nil
		}
		if existing.WorkosID.Valid && existing.WorkosID.String != user.ID {
			return "", fmt.Errorf("local user %q is already linked to different WorkOS user %q", existing.ID, existing.WorkosID.String)
		}
	}
	if existing.WorkosUpdatedAt.Valid && !shouldProcessEvent(nil, &existing.WorkosUpdatedAt.Time, "", updatedAt) {
		return "stale_skip", nil
	}
	if existing.Email == user.Email &&
		existing.DisplayName == displayNameFromWorkOSUser(user) &&
		pgTextEmptyEqual(existing.PhotoUrl, user.ProfilePictureURL) &&
		existing.WorkosID.Valid &&
		existing.WorkosID.String == user.ID &&
		pgTimeEqual(existing.WorkosUpdatedAt, updatedAt) &&
		!existing.DeletedAt.Valid &&
		!existing.WorkosDeletedAt.Valid {
		return "noop", nil
	}
	return "update", nil
}

func classifyMembershipChanges(ctx context.Context, db *pgxpool.Pool, organizationID string, skipped bool, users map[string]workos.User, members []workos.Member) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	if skipped {
		return counts, nil
	}
	for _, member := range members {
		gramUserID, resolved, err := expectedGramUserID(ctx, db, users[member.UserID])
		if err != nil {
			return changeCounts{}, err
		}
		if !resolved {
			counts.StaleSkip++
			continue
		}
		updatedAt, err := parseWorkOSTime(member.UpdatedAt)
		if err != nil {
			counts.StaleSkip++
			continue
		}
		change, err := classifyMembershipRow(ctx, db, organizationID, member, gramUserID, updatedAt)
		if err != nil {
			return changeCounts{}, err
		}
		counts = addChange(counts, change)
	}
	return counts, nil
}

func classifyMembershipRow(ctx context.Context, db *pgxpool.Pool, organizationID string, member workos.Member, gramUserID string, updatedAt time.Time) (string, error) {
	var userID pgtype.Text
	var workosUserID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var deletedAt pgtype.Timestamptz
	err := db.QueryRow(ctx, `
SELECT user_id, workos_user_id, workos_updated_at, workos_last_event_id, deleted_at
FROM organization_user_relationships
WHERE organization_id = $1
  AND workos_membership_id = $2
ORDER BY updated_at DESC
LIMIT 1`, organizationID, member.ID).Scan(&userID, &workosUserID, &rowUpdatedAt, &lastEventID, &deletedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		missingIDChange, err := classifyMissingMembershipIDRepair(ctx, db, organizationID, member, gramUserID, updatedAt)
		if err != nil {
			return "", err
		}
		return missingIDChange, nil
	case err != nil:
		return "", fmt.Errorf("query local membership %q: %w", member.ID, err)
	}
	if membershipNeedsMissingFieldRepair(userID, workosUserID, rowUpdatedAt, deletedAt, gramUserID, member) {
		return "update", nil
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return "stale_skip", nil
	}
	if deletedAt.Valid ||
		!userID.Valid ||
		userID.String != gramUserID ||
		!workosUserID.Valid ||
		workosUserID.String != member.UserID ||
		!pgTimeEqual(rowUpdatedAt, updatedAt) {
		return "update", nil
	}
	return "noop", nil
}

func classifyMissingMembershipIDRepair(ctx context.Context, db *pgxpool.Pool, organizationID string, member workos.Member, gramUserID string, updatedAt time.Time) (string, error) {
	var workosMembershipID pgtype.Text
	var workosUserID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var deletedAt pgtype.Timestamptz
	err := db.QueryRow(ctx, `
SELECT workos_membership_id, workos_user_id, workos_updated_at, workos_last_event_id, deleted_at
FROM organization_user_relationships
WHERE organization_id = $1
  AND user_id = $2
ORDER BY updated_at DESC
LIMIT 1`, organizationID, conv.ToPGText(gramUserID)).Scan(&workosMembershipID, &workosUserID, &rowUpdatedAt, &lastEventID, &deletedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "create", nil
	case err != nil:
		return "", fmt.Errorf("query local membership by user %q: %w", member.ID, err)
	}
	if deletedAt.Valid {
		return "update", nil
	}
	if !workosMembershipID.Valid {
		return "update", nil
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return "stale_skip", nil
	}
	if workosMembershipID.String != member.ID ||
		!workosUserID.Valid ||
		workosUserID.String != member.UserID ||
		!pgTimeEqual(rowUpdatedAt, updatedAt) {
		return "update", nil
	}
	return "noop", nil
}

func classifyAssignmentChanges(ctx context.Context, db *pgxpool.Pool, organizationID string, skipped bool, roles []workos.Role, users map[string]workos.User, members []workos.Member) (changeCounts, error) {
	counts := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	if skipped {
		return counts, nil
	}
	for _, member := range members {
		gramUserID, resolved, err := expectedGramUserID(ctx, db, users[member.UserID])
		if err != nil {
			return changeCounts{}, err
		}
		if !resolved {
			counts.StaleSkip++
			continue
		}
		if member.RoleSlug != "" {
			available, err := plannedAssignmentRoleAvailable(ctx, db, organizationID, roles, member.RoleSlug)
			if err != nil {
				return changeCounts{}, err
			}
			if !available {
				counts.StaleSkip++
				continue
			}
		}
		activeAssignments, missingUserAssignments, err := countActiveAssignments(ctx, db, organizationID, member.ID, gramUserID)
		if err != nil {
			return changeCounts{}, err
		}
		if member.RoleSlug == "" {
			if activeAssignments > 0 {
				counts.Delete += activeAssignments
			} else {
				counts.Noop++
			}
			continue
		}
		if activeAssignments == 0 {
			counts.Create++
		} else if missingUserAssignments > 0 {
			counts.Update += missingUserAssignments
			counts.Noop += activeAssignments - missingUserAssignments
		} else {
			counts.Noop += activeAssignments
		}
	}
	return counts, nil
}

func countActiveAssignments(ctx context.Context, db *pgxpool.Pool, organizationID, membershipID, gramUserID string) (int, int, error) {
	var count int
	var missingUserCount int
	if err := db.QueryRow(ctx, `
SELECT count(*)::int
FROM organization_role_assignments
WHERE organization_id = $1
  AND workos_membership_id = $2
  AND deleted_at IS NULL`, organizationID, membershipID).Scan(&count); err != nil {
		return 0, 0, fmt.Errorf("count active role assignments for membership %q: %w", membershipID, err)
	}
	if err := db.QueryRow(ctx, `
SELECT count(*)::int
FROM organization_role_assignments
WHERE organization_id = $1
  AND workos_membership_id = $2
  AND deleted_at IS NULL
  AND (user_id IS NULL OR user_id <> $3)`, organizationID, membershipID, gramUserID).Scan(&missingUserCount); err != nil {
		return 0, 0, fmt.Errorf("count role assignments missing user for membership %q: %w", membershipID, err)
	}
	return count, missingUserCount, nil
}

func plannedAssignmentRoleAvailable(ctx context.Context, db *pgxpool.Pool, organizationID string, roles []workos.Role, roleSlug string) (bool, error) {
	for _, role := range roles {
		if role.Slug != roleSlug || role.Type != "OrganizationRole" {
			continue
		}
		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return false, fmt.Errorf("parse organization role %q updated_at: %w", role.Slug, err)
		}
		change, err := classifyRoleRow(ctx, db, "organization_roles", "organization_id = $1", []any{organizationID}, role, updatedAt)
		if err != nil {
			return false, fmt.Errorf("classify organization role %q: %w", role.Slug, err)
		}
		if change == "create" || change == "update" || change == "noop" {
			return true, nil
		}
		return activeAssignmentRoleExists(ctx, db, organizationID, roleSlug)
	}

	return activeGlobalRoleExists(ctx, db, roleSlug)
}

func activeAssignmentRoleExists(ctx context.Context, db queryRower, organizationID string, roleSlug string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM organization_roles
  WHERE organization_id = $1
    AND workos_slug = $2
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
) OR EXISTS (
  SELECT 1
  FROM global_roles
  WHERE workos_slug = $2
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
)`, organizationID, roleSlug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check active role for assignment slug %q: %w", roleSlug, err)
	}
	return exists, nil
}

func activeGlobalRoleExists(ctx context.Context, db queryRower, roleSlug string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM global_roles
  WHERE workos_slug = $1
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
)`, roleSlug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check active global role slug %q: %w", roleSlug, err)
	}
	return exists, nil
}

func addChange(counts changeCounts, change string) changeCounts {
	switch change {
	case "create":
		counts.Create++
	case "update":
		counts.Update++
	case "noop":
		counts.Noop++
	case "delete":
		counts.Delete++
	case "stale_skip":
		counts.StaleSkip++
	}
	return counts
}

func collectGlobalRoleChangeDetails(ctx context.Context, db *pgxpool.Pool, roles []workos.Role) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	snapshotSlugs := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		snapshotSlugs[role.Slug] = struct{}{}
		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse global role %q updated_at: %w", role.Slug, err)
		}

		detail, ok, err := collectGlobalRoleUpdateDetail(ctx, db, role, updatedAt)
		if err != nil {
			return nil, err
		}
		if ok {
			details = append(details, detail)
			continue
		}
		change, err := classifyRoleRow(ctx, db, "global_roles", "TRUE", nil, role, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("classify global role %q: %w", role.Slug, err)
		}
		if change == "create" {
			details = append(details, roleCreateDetail("global_role", role, updatedAt))
		}
	}

	deleteDetails, err := collectMissingGlobalRoleDeleteDetails(ctx, db, snapshotSlugs)
	if err != nil {
		return nil, err
	}
	return append(details, deleteDetails...), nil
}

func collectGlobalRoleUpdateDetail(ctx context.Context, db *pgxpool.Pool, role workos.Role, updatedAt time.Time) (changeDetail, bool, error) {
	var name string
	var description pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var workosDeleted bool
	var deleted bool
	err := db.QueryRow(ctx, `
SELECT workos_name, workos_description, workos_updated_at, workos_last_event_id, workos_deleted, deleted
FROM global_roles
WHERE workos_slug = $1
LIMIT 1`, role.Slug).Scan(&name, &description, &rowUpdatedAt, &lastEventID, &workosDeleted, &deleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query local global role %q: %w", role.Slug, err)
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}

	fields := make([]fieldChange, 0)
	fields = appendFieldChange(fields, "workos_name", name, role.Name)
	if !pgTextEmptyEqual(description, role.Description) {
		fields = appendFieldChange(fields, "workos_description", pgTextDisplay(description), role.Description)
	}
	fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	fields = appendFieldChange(fields, "workos_deleted", boolDisplay(workosDeleted), "false")
	fields = appendFieldChange(fields, "deleted", boolDisplay(deleted), "false")
	if len(fields) == 0 {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	return changeDetail{Entity: "global_role", ID: role.Slug, Action: "update", Fields: fields}, true, nil
}

func collectMissingGlobalRoleDeleteDetails(ctx context.Context, db *pgxpool.Pool, snapshotSlugs map[string]struct{}) ([]changeDetail, error) {
	rows, err := db.Query(ctx, `
SELECT workos_slug, workos_updated_at, workos_last_event_id
FROM global_roles
WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query local global roles: %w", err)
	}
	defer rows.Close()

	details := make([]changeDetail, 0)
	now := time.Now().UTC()
	for rows.Next() {
		var slug string
		var updatedAt pgtype.Timestamptz
		var lastEventID pgtype.Text
		if err := rows.Scan(&slug, &updatedAt, &lastEventID); err != nil {
			return nil, fmt.Errorf("scan local global role: %w", err)
		}
		if _, ok := snapshotSlugs[slug]; ok {
			continue
		}
		if shouldProcessEvent(textPtr(lastEventID), timePtr(updatedAt), "", now) {
			details = append(details, changeDetail{
				Entity: "global_role",
				ID:     slug,
				Action: "delete",
				Fields: []fieldChange{{
					Name:   "deleted_at",
					Before: "<null>",
					After:  "now",
				}},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate local global roles: %w", err)
	}
	return details, nil
}

func collectOrganizationChangeDetails(ctx context.Context, db *pgxpool.Pool, org workos.Organization, gramOrgID string, skipped bool, roles []workos.Role, users map[string]workos.User, members []workos.Member) ([]changeDetail, error) {
	if skipped {
		return nil, nil
	}
	details := make([]changeDetail, 0)

	orgDetail, ok, err := collectOrganizationUpdateDetail(ctx, db, org, gramOrgID)
	if err != nil {
		return nil, err
	}
	if ok {
		details = append(details, orgDetail)
	} else {
		orgChanges, err := classifyOrganizationMetadataChange(ctx, db, org, gramOrgID, skipped)
		if err != nil {
			return nil, err
		}
		if orgChanges.Create > 0 {
			details = append(details, organizationCreateDetail(org, gramOrgID))
		}
	}

	roleDetails, err := collectOrganizationRoleChangeDetails(ctx, db, gramOrgID, roles)
	if err != nil {
		return nil, err
	}
	details = append(details, roleDetails...)

	userDetails, err := collectUserChangeDetails(ctx, db, users)
	if err != nil {
		return nil, err
	}
	details = append(details, userDetails...)

	membershipDetails, err := collectMembershipChangeDetails(ctx, db, gramOrgID, users, members)
	if err != nil {
		return nil, err
	}
	details = append(details, membershipDetails...)

	assignmentDetails, err := collectAssignmentChangeDetails(ctx, db, gramOrgID, roles, users, members)
	if err != nil {
		return nil, err
	}
	details = append(details, assignmentDetails...)

	return details, nil
}

func collectOrganizationUpdateDetail(ctx context.Context, db *pgxpool.Pool, org workos.Organization, gramOrgID string) (changeDetail, bool, error) {
	updatedAt, err := parseWorkOSTime(org.UpdatedAt)
	if err != nil {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("parse organization %q updated_at: %w", org.ID, err)
	}

	var existingID string
	var name string
	var slug string
	var workosID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var disabledAt pgtype.Timestamptz
	err = db.QueryRow(ctx, `
SELECT id, name, slug, workos_id, workos_updated_at, workos_last_event_id, disabled_at
FROM organization_metadata
WHERE workos_id = $1 OR id = $2
ORDER BY CASE WHEN workos_id = $1 THEN 0 ELSE 1 END
LIMIT 1`, org.ID, gramOrgID).Scan(&existingID, &name, &slug, &workosID, &rowUpdatedAt, &lastEventID, &disabledAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query organization metadata %q: %w", org.ID, err)
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}

	fields := make([]fieldChange, 0)
	fields = appendFieldChange(fields, "id", existingID, gramOrgID)
	fields = appendFieldChange(fields, "name", name, org.Name)
	if slug == "" {
		fields = appendFieldChange(fields, "slug", "<empty>", "generated unique slug")
	}
	fields = appendFieldChange(fields, "workos_id", pgTextDisplay(workosID), org.ID)
	fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	if disabledAt.Valid {
		fields = appendFieldChange(fields, "disabled_at", pgTimeDisplay(disabledAt), "<null>")
	}
	if len(fields) == 0 {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	return changeDetail{Entity: "organization", ID: org.ID, Action: "update", Fields: fields}, true, nil
}

func collectOrganizationRoleChangeDetails(ctx context.Context, db *pgxpool.Pool, organizationID string, roles []workos.Role) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	snapshotSlugs := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role.Type != "OrganizationRole" {
			continue
		}
		snapshotSlugs[role.Slug] = struct{}{}
		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse organization role %q updated_at: %w", role.Slug, err)
		}
		detail, ok, err := collectOrganizationRoleUpdateDetail(ctx, db, organizationID, role, updatedAt)
		if err != nil {
			return nil, err
		}
		if ok {
			details = append(details, detail)
			continue
		}
		change, err := classifyRoleRow(ctx, db, "organization_roles", "organization_id = $1", []any{organizationID}, role, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("classify organization role %q: %w", role.Slug, err)
		}
		if change == "create" {
			details = append(details, roleCreateDetail("organization_role", role, updatedAt))
		}
	}
	deleteDetails, err := collectMissingOrganizationRoleDeleteDetails(ctx, db, organizationID, snapshotSlugs)
	if err != nil {
		return nil, err
	}
	return append(details, deleteDetails...), nil
}

func collectOrganizationRoleUpdateDetail(ctx context.Context, db *pgxpool.Pool, organizationID string, role workos.Role, updatedAt time.Time) (changeDetail, bool, error) {
	var name string
	var description pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var workosDeleted bool
	var deleted bool
	err := db.QueryRow(ctx, `
SELECT workos_name, workos_description, workos_updated_at, workos_last_event_id, workos_deleted, deleted
FROM organization_roles
WHERE organization_id = $1
  AND workos_slug = $2
LIMIT 1`, organizationID, role.Slug).Scan(&name, &description, &rowUpdatedAt, &lastEventID, &workosDeleted, &deleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query local organization role %q: %w", role.Slug, err)
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}

	fields := make([]fieldChange, 0)
	fields = appendFieldChange(fields, "workos_name", name, role.Name)
	if !pgTextEmptyEqual(description, role.Description) {
		fields = appendFieldChange(fields, "workos_description", pgTextDisplay(description), role.Description)
	}
	fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	fields = appendFieldChange(fields, "workos_deleted", boolDisplay(workosDeleted), "false")
	fields = appendFieldChange(fields, "deleted", boolDisplay(deleted), "false")
	if len(fields) == 0 {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	return changeDetail{Entity: "organization_role", ID: role.Slug, Action: "update", Fields: fields}, true, nil
}

func collectMissingOrganizationRoleDeleteDetails(ctx context.Context, db *pgxpool.Pool, organizationID string, snapshotSlugs map[string]struct{}) ([]changeDetail, error) {
	rows, err := db.Query(ctx, `
SELECT workos_slug, workos_updated_at, workos_last_event_id
FROM organization_roles
WHERE organization_id = $1
  AND deleted_at IS NULL`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("query local organization roles: %w", err)
	}
	defer rows.Close()

	details := make([]changeDetail, 0)
	now := time.Now().UTC()
	for rows.Next() {
		var slug string
		var updatedAt pgtype.Timestamptz
		var lastEventID pgtype.Text
		if err := rows.Scan(&slug, &updatedAt, &lastEventID); err != nil {
			return nil, fmt.Errorf("scan local organization role: %w", err)
		}
		if _, ok := snapshotSlugs[slug]; ok {
			continue
		}
		if shouldProcessEvent(textPtr(lastEventID), timePtr(updatedAt), "", now) {
			details = append(details, changeDetail{
				Entity: "organization_role",
				ID:     slug,
				Action: "delete",
				Fields: []fieldChange{{
					Name:   "deleted_at",
					Before: "<null>",
					After:  "now",
				}},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate local organization roles: %w", err)
	}
	return details, nil
}

func collectUserChangeDetails(ctx context.Context, db *pgxpool.Pool, users map[string]workos.User) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	for _, user := range users {
		updatedAt, err := parseWorkOSTime(user.UpdatedAt)
		if err != nil {
			continue
		}
		createdAt, err := parseWorkOSTime(user.CreatedAt)
		if err != nil {
			continue
		}
		existing, found, err := findUserByWorkOSID(ctx, db, user.ID)
		if err != nil {
			return nil, err
		}
		if !found && user.ExternalID != "" {
			existing, found, err = findUserByID(ctx, db, user.ExternalID)
			if err != nil {
				return nil, err
			}
			if found && existing.WorkosID.Valid && existing.WorkosID.String != user.ID {
				return nil, fmt.Errorf("local user %q is already linked to different WorkOS user %q", existing.ID, existing.WorkosID.String)
			}
		}
		if !found {
			if user.ExternalID != "" {
				details = append(details, userCreateDetail(user, user.ExternalID, createdAt, updatedAt))
			}
			continue
		}
		if existing.WorkosUpdatedAt.Valid && !shouldProcessEvent(nil, &existing.WorkosUpdatedAt.Time, "", updatedAt) {
			continue
		}
		fields := make([]fieldChange, 0)
		fields = appendFieldChange(fields, "email", existing.Email, user.Email)
		fields = appendFieldChange(fields, "display_name", existing.DisplayName, displayNameFromWorkOSUser(user))
		if !pgTextEmptyEqual(existing.PhotoUrl, user.ProfilePictureURL) {
			fields = appendFieldChange(fields, "photo_url", pgTextDisplay(existing.PhotoUrl), user.ProfilePictureURL)
		}
		fields = appendFieldChange(fields, "workos_id", pgTextDisplay(existing.WorkosID), user.ID)
		if !existing.WorkosCreatedAt.Valid {
			fields = appendFieldChange(fields, "workos_created_at", pgTimeDisplay(existing.WorkosCreatedAt), timeDisplay(createdAt))
		}
		fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(existing.WorkosUpdatedAt), timeDisplay(updatedAt))
		if existing.DeletedAt.Valid {
			fields = appendFieldChange(fields, "deleted_at", pgTimeDisplay(existing.DeletedAt), "<null>")
		}
		if existing.WorkosDeletedAt.Valid {
			fields = appendFieldChange(fields, "workos_deleted_at", pgTimeDisplay(existing.WorkosDeletedAt), "<null>")
		}
		if len(fields) > 0 {
			details = append(details, changeDetail{Entity: "user", ID: user.ID, Action: "update", Fields: fields})
		}
	}
	return details, nil
}

func collectMembershipChangeDetails(ctx context.Context, db *pgxpool.Pool, organizationID string, users map[string]workos.User, members []workos.Member) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	for _, member := range members {
		gramUserID, resolved, err := expectedGramUserID(ctx, db, users[member.UserID])
		if err != nil {
			return nil, err
		}
		if !resolved {
			continue
		}
		updatedAt, err := parseWorkOSTime(member.UpdatedAt)
		if err != nil {
			continue
		}
		detail, ok, err := collectMembershipUpdateDetail(ctx, db, organizationID, member, gramUserID, updatedAt)
		if err != nil {
			return nil, err
		}
		if ok {
			details = append(details, detail)
			continue
		}
		change, err := classifyMembershipRow(ctx, db, organizationID, member, gramUserID, updatedAt)
		if err != nil {
			return nil, err
		}
		if change == "create" {
			details = append(details, membershipCreateDetail(organizationID, member, gramUserID, updatedAt))
		}
	}
	return details, nil
}

func collectMembershipUpdateDetail(ctx context.Context, db *pgxpool.Pool, organizationID string, member workos.Member, gramUserID string, updatedAt time.Time) (changeDetail, bool, error) {
	var userID pgtype.Text
	var workosUserID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var deletedAt pgtype.Timestamptz
	err := db.QueryRow(ctx, `
SELECT user_id, workos_user_id, workos_updated_at, workos_last_event_id, deleted_at
FROM organization_user_relationships
WHERE organization_id = $1
  AND workos_membership_id = $2
ORDER BY updated_at DESC
LIMIT 1`, organizationID, member.ID).Scan(&userID, &workosUserID, &rowUpdatedAt, &lastEventID, &deletedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return collectMissingMembershipIDRepairDetail(ctx, db, organizationID, member, gramUserID, updatedAt)
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query local membership %q: %w", member.ID, err)
	}
	if membershipNeedsMissingFieldRepair(userID, workosUserID, rowUpdatedAt, deletedAt, gramUserID, member) {
		fields := missingMembershipFieldRepairs(userID, workosUserID, rowUpdatedAt, gramUserID, member, updatedAt)
		return changeDetail{Entity: "membership", ID: member.ID, Action: "update", Fields: fields}, true, nil
	}
	if !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}

	fields := make([]fieldChange, 0)
	fields = appendFieldChange(fields, "user_id", pgTextDisplay(userID), gramUserID)
	fields = appendFieldChange(fields, "workos_user_id", pgTextDisplay(workosUserID), member.UserID)
	fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	if deletedAt.Valid {
		fields = appendFieldChange(fields, "deleted_at", pgTimeDisplay(deletedAt), "<null>")
	}
	if len(fields) == 0 {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	return changeDetail{Entity: "membership", ID: member.ID, Action: "update", Fields: fields}, true, nil
}

func collectMissingMembershipIDRepairDetail(ctx context.Context, db *pgxpool.Pool, organizationID string, member workos.Member, gramUserID string, updatedAt time.Time) (changeDetail, bool, error) {
	var workosMembershipID pgtype.Text
	var workosUserID pgtype.Text
	var rowUpdatedAt pgtype.Timestamptz
	var lastEventID pgtype.Text
	var deletedAt pgtype.Timestamptz
	err := db.QueryRow(ctx, `
SELECT workos_membership_id, workos_user_id, workos_updated_at, workos_last_event_id, deleted_at
FROM organization_user_relationships
WHERE organization_id = $1
  AND user_id = $2
ORDER BY updated_at DESC
LIMIT 1`, organizationID, conv.ToPGText(gramUserID)).Scan(&workosMembershipID, &workosUserID, &rowUpdatedAt, &lastEventID, &deletedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query local membership by user %q: %w", member.ID, err)
	}
	if deletedAt.Valid {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	if workosMembershipID.Valid && !shouldProcessEvent(textPtr(lastEventID), timePtr(rowUpdatedAt), "", updatedAt) {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}

	fields := make([]fieldChange, 0)
	if !workosMembershipID.Valid {
		fields = appendFieldChange(fields, "workos_membership_id", pgTextDisplay(workosMembershipID), member.ID)
	}
	if !workosUserID.Valid && member.UserID != "" {
		fields = appendFieldChange(fields, "workos_user_id", pgTextDisplay(workosUserID), member.UserID)
	}
	if !rowUpdatedAt.Valid {
		fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	}
	if len(fields) == 0 {
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	}
	return changeDetail{Entity: "membership", ID: member.ID, Action: "update", Fields: fields}, true, nil
}

func membershipNeedsMissingFieldRepair(userID pgtype.Text, workosUserID pgtype.Text, rowUpdatedAt pgtype.Timestamptz, deletedAt pgtype.Timestamptz, gramUserID string, member workos.Member) bool {
	if deletedAt.Valid {
		return false
	}
	return !userID.Valid && gramUserID != "" ||
		!workosUserID.Valid && member.UserID != "" ||
		!rowUpdatedAt.Valid
}

func missingMembershipFieldRepairs(userID pgtype.Text, workosUserID pgtype.Text, rowUpdatedAt pgtype.Timestamptz, gramUserID string, member workos.Member, updatedAt time.Time) []fieldChange {
	fields := make([]fieldChange, 0)
	if !userID.Valid && gramUserID != "" {
		fields = appendFieldChange(fields, "user_id", pgTextDisplay(userID), gramUserID)
	}
	if !workosUserID.Valid && member.UserID != "" {
		fields = appendFieldChange(fields, "workos_user_id", pgTextDisplay(workosUserID), member.UserID)
	}
	if !rowUpdatedAt.Valid {
		fields = appendFieldChange(fields, "workos_updated_at", pgTimeDisplay(rowUpdatedAt), timeDisplay(updatedAt))
	}
	return fields
}

func collectAssignmentChangeDetails(ctx context.Context, db *pgxpool.Pool, organizationID string, roles []workos.Role, users map[string]workos.User, members []workos.Member) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	for _, member := range members {
		gramUserID, resolved, err := expectedGramUserID(ctx, db, users[member.UserID])
		if err != nil {
			return nil, err
		}
		if !resolved {
			continue
		}
		roleAvailable := false
		if member.RoleSlug != "" {
			roleAvailable, err = plannedAssignmentRoleAvailable(ctx, db, organizationID, roles, member.RoleSlug)
			if err != nil {
				return nil, err
			}
			if !roleAvailable {
				continue
			}
		}
		rows, err := db.Query(ctx, `
SELECT id, user_id
FROM organization_role_assignments
WHERE organization_id = $1
  AND workos_membership_id = $2
  AND deleted_at IS NULL`, organizationID, member.ID)
		if err != nil {
			return nil, fmt.Errorf("query active role assignments for membership %q: %w", member.ID, err)
		}
		activeAssignments := 0
		for rows.Next() {
			var id pgtype.UUID
			var userID pgtype.Text
			if err := rows.Scan(&id, &userID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan role assignment for membership %q: %w", member.ID, err)
			}
			activeAssignments++
			if member.RoleSlug == "" {
				details = append(details, changeDetail{
					Entity: "role_assignment",
					ID:     uuidDisplay(id),
					Action: "delete",
					Fields: []fieldChange{{
						Name:   "deleted_at",
						Before: "<null>",
						After:  "now",
					}},
				})
				continue
			}
			if !userID.Valid || userID.String != gramUserID {
				details = append(details, changeDetail{
					Entity: "role_assignment",
					ID:     uuidDisplay(id),
					Action: "update",
					Fields: []fieldChange{{
						Name:   "user_id",
						Before: pgTextDisplay(userID),
						After:  gramUserID,
					}},
				})
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate role assignments for membership %q: %w", member.ID, err)
		}
		rows.Close()
		if activeAssignments == 0 && member.RoleSlug != "" && roleAvailable {
			details = append(details, roleAssignmentCreateDetail(organizationID, member, gramUserID))
		}
	}
	return details, nil
}

func runOrganizationBackfill(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, workosClient Client, opts options, orgs []orgExpectation) report {
	rep := report{
		scanned:            len(orgs),
		skipped:            0,
		skippedNoop:        0,
		written:            0,
		validated:          0,
		failed:             0,
		validationFailures: 0,
		organizationRows:   changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		roleRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		userRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		membershipRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		assignmentRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
	}
	backfill := NewBackfillWorkOSOrganization(logger, db, workosClient)
	for i, org := range orgs {
		if org.skipped {
			rep.skipped++
			fmt.Printf("[%d/%d] skip %s: no local row and no WorkOS external_id\n", i+1, len(orgs), org.workosOrgID)
			continue
		}
		if plannedOrganizationMutation(org) == 0 {
			rep.skippedNoop++
			fmt.Printf("[%d/%d] skip noop %s -> %s\n", i+1, len(orgs), org.workosOrgID, org.gramOrgID)
			continue
		}

		fmt.Printf("[%d/%d] backfill %s -> %s\n", i+1, len(orgs), org.workosOrgID, org.gramOrgID)
		if err := backfill.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: org.workosOrgID}); err != nil {
			rep.failed++
			fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
		} else {
			rep.written++
			rep.organizationRows = rep.organizationRows.Add(org.orgChanges)
			rep.roleRows = rep.roleRows.Add(org.roleChanges)
			rep.userRows = rep.userRows.Add(org.userChanges)
			rep.membershipRows = rep.membershipRows.Add(org.membershipChanges)
			rep.assignmentRows = rep.assignmentRows.Add(org.assignmentChanges)
			if err := validateOrganization(ctx, db, org); err != nil {
				rep.validationFailures++
				fmt.Fprintf(os.Stderr, "  validation failed: %v\n", err)
			} else {
				rep.validated++
			}
		}

		if opts.pauseAfterEach {
			waitForEnter("Paused after organization. Press Enter to continue.")
		}
	}
	return rep
}

func validateOrganizations(ctx context.Context, db *pgxpool.Pool, orgs []orgExpectation) report {
	rep := report{
		scanned:            len(orgs),
		skipped:            0,
		skippedNoop:        0,
		written:            0,
		validated:          0,
		failed:             0,
		validationFailures: 0,
		organizationRows:   changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		roleRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		userRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		membershipRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		assignmentRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
	}
	for _, org := range orgs {
		if org.skipped {
			rep.skipped++
			continue
		}
		if err := validateOrganization(ctx, db, org); err != nil {
			rep.validationFailures++
			fmt.Fprintf(os.Stderr, "validation failed for %s: %v\n", org.workosOrgID, err)
			continue
		}
		rep.validated++
	}
	return rep
}

func validateOrganization(ctx context.Context, db *pgxpool.Pool, org orgExpectation) error {
	var gramOrgID string
	if err := db.QueryRow(ctx, "SELECT id FROM organization_metadata WHERE workos_id = $1 LIMIT 1", org.workosOrgID).Scan(&gramOrgID); err != nil {
		return fmt.Errorf("organization_metadata missing workos_id=%s: %w", org.workosOrgID, err)
	}
	if gramOrgID != org.gramOrgID {
		return fmt.Errorf("organization id mismatch: got %s, expected %s", gramOrgID, org.gramOrgID)
	}

	expectedRoleSlugs := make([]string, 0, len(org.roles))
	for _, role := range org.roles {
		if role.Type == "OrganizationRole" {
			expectedRoleSlugs = append(expectedRoleSlugs, role.Slug)
		}
	}
	if err := requireSlugs(ctx, db, "organization_roles", "organization_id = $1", []any{gramOrgID}, expectedRoleSlugs); err != nil {
		return fmt.Errorf("organization roles: %w", err)
	}

	expectedUserIDs := make([]string, 0, len(org.users))
	resolvableWorkOSUserIDs := make(map[string]struct{}, len(org.users))
	for _, user := range org.users {
		gramUserID, ok, err := expectedGramUserID(ctx, db, user)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		expectedUserIDs = append(expectedUserIDs, user.ID)
		resolvableWorkOSUserIDs[user.ID] = struct{}{}
		_ = gramUserID
	}
	if err := requireUsers(ctx, db, expectedUserIDs); err != nil {
		return err
	}

	membershipIDs := make([]string, 0, len(org.members))
	roleMembershipIDs := make([]string, 0, len(org.members))
	for _, member := range org.members {
		if _, ok := resolvableWorkOSUserIDs[member.UserID]; !ok {
			continue
		}
		membershipIDs = append(membershipIDs, member.ID)
		if member.RoleSlug != "" {
			roleExists, err := activeAssignmentRoleExists(ctx, db, gramOrgID, member.RoleSlug)
			if err != nil {
				return err
			}
			if roleExists {
				roleMembershipIDs = append(roleMembershipIDs, member.ID)
			}
		}
	}
	if err := requireMemberships(ctx, db, gramOrgID, membershipIDs); err != nil {
		return err
	}
	if err := requireRoleAssignments(ctx, db, gramOrgID, roleMembershipIDs); err != nil {
		return err
	}

	return nil
}

func expectedGramUserID(ctx context.Context, db *pgxpool.Pool, user workos.User) (string, bool, error) {
	if user.ID == "" {
		return "", false, nil
	}
	existing, found, err := findUserByWorkOSID(ctx, db, user.ID)
	if err != nil {
		return "", false, err
	}
	if found {
		return existing.ID, true, nil
	}
	if user.ExternalID == "" {
		return "", false, nil
	}
	return user.ExternalID, true, nil
}

func validateGlobalRoles(ctx context.Context, db *pgxpool.Pool, roles []workos.Role) report {
	rep := report{
		scanned:            len(roles),
		skipped:            0,
		skippedNoop:        0,
		written:            0,
		validated:          0,
		failed:             0,
		validationFailures: 0,
		organizationRows:   changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		roleRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		userRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		membershipRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		assignmentRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
	}
	expectedSlugs := make([]string, 0, len(roles))
	for _, role := range roles {
		expectedSlugs = append(expectedSlugs, role.Slug)
	}
	if err := requireSlugs(ctx, db, "global_roles", "TRUE", nil, expectedSlugs); err != nil {
		rep.validationFailures = 1
		fmt.Fprintf(os.Stderr, "global role validation failed: %v\n", err)
		return rep
	}
	rep.validated = len(roles)
	return rep
}

func requireSlugs(ctx context.Context, db *pgxpool.Pool, table, predicate string, args []any, expected []string) error {
	expectedSet := set(expected)
	query := fmt.Sprintf("SELECT workos_slug FROM %s WHERE %s AND deleted IS FALSE AND workos_deleted IS FALSE", table, predicate) // #nosec G201 -- table and predicate are fixed call-site constants.
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query active role slugs: %w", err)
	}
	defer rows.Close()

	actualSet := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return fmt.Errorf("scan role slug: %w", err)
		}
		actualSet[slug] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate role slugs: %w", err)
	}

	missing := difference(expectedSet, actualSet)
	extra := difference(actualSet, expectedSet)
	if len(missing) > 0 || len(extra) > 0 {
		return fmt.Errorf("slug mismatch: missing=%v extra=%v", missing, extra)
	}
	return nil
}

func requireMemberships(ctx context.Context, db *pgxpool.Pool, orgID string, membershipIDs []string) error {
	if len(membershipIDs) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
SELECT workos_membership_id
FROM organization_user_relationships
WHERE organization_id = $1
  AND workos_membership_id = ANY($2::text[])
  AND deleted IS FALSE`, orgID, membershipIDs)
	if err != nil {
		return fmt.Errorf("query active memberships: %w", err)
	}
	defer rows.Close()

	actual := map[string]bool{}
	for rows.Next() {
		var membershipID string
		if err := rows.Scan(&membershipID); err != nil {
			return fmt.Errorf("scan membership id: %w", err)
		}
		actual[membershipID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate memberships: %w", err)
	}

	if missing := difference(set(membershipIDs), actual); len(missing) > 0 {
		return fmt.Errorf("missing memberships: %v", missing)
	}
	return nil
}

func requireUsers(ctx context.Context, db *pgxpool.Pool, workosUserIDs []string) error {
	if len(workosUserIDs) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
SELECT workos_id
FROM users
WHERE workos_id = ANY($1::text[])
  AND deleted_at IS NULL`, workosUserIDs)
	if err != nil {
		return fmt.Errorf("query active users: %w", err)
	}
	defer rows.Close()

	actual := map[string]bool{}
	for rows.Next() {
		var workosUserID string
		if err := rows.Scan(&workosUserID); err != nil {
			return fmt.Errorf("scan user workos id: %w", err)
		}
		actual[workosUserID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate users: %w", err)
	}

	if missing := difference(set(workosUserIDs), actual); len(missing) > 0 {
		return fmt.Errorf("missing users: %v", missing)
	}
	return nil
}

func requireRoleAssignments(ctx context.Context, db *pgxpool.Pool, orgID string, membershipIDs []string) error {
	if len(membershipIDs) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
SELECT DISTINCT workos_membership_id
FROM organization_role_assignments
WHERE organization_id = $1
  AND workos_membership_id = ANY($2::text[])
  AND deleted_at IS NULL`, orgID, membershipIDs)
	if err != nil {
		return fmt.Errorf("query active role assignments: %w", err)
	}
	defer rows.Close()

	actual := map[string]bool{}
	for rows.Next() {
		var membershipID string
		if err := rows.Scan(&membershipID); err != nil {
			return fmt.Errorf("scan role assignment membership id: %w", err)
		}
		actual[membershipID] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate role assignments: %w", err)
	}

	if missing := difference(set(membershipIDs), actual); len(missing) > 0 {
		return fmt.Errorf("missing role assignments for memberships: %v", missing)
	}
	return nil
}

func printOrganizationPlan(orgs []orgExpectation) {
	var roles int
	var users int
	var memberships int
	var skipped int
	orgChanges := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	roleChanges := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	userChanges := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	membershipChanges := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	assignmentChanges := changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}
	changeDetails := make([]changeDetail, 0)
	for _, org := range orgs {
		if org.skipped {
			skipped++
		}
		orgChanges = orgChanges.Add(org.orgChanges)
		roleChanges = roleChanges.Add(org.roleChanges)
		userChanges = userChanges.Add(org.userChanges)
		membershipChanges = membershipChanges.Add(org.membershipChanges)
		assignmentChanges = assignmentChanges.Add(org.assignmentChanges)
		for _, role := range org.roles {
			if role.Type == "OrganizationRole" {
				roles++
			}
		}
		users += len(org.users)
		memberships += len(org.members)
		changeDetails = append(changeDetails, org.changeDetails...)
	}

	fmt.Println("Organization preflight:")
	fmt.Printf("  workos_orgs: %d\n", len(orgs))
	fmt.Printf("  expected_organization_roles: %d\n", roles)
	fmt.Printf("  expected_users: %d\n", users)
	fmt.Printf("  expected_memberships: %d\n", memberships)
	fmt.Printf("  skipped_unlinked_without_external_id: %d\n", skipped)
	printChangeCounts("  organization_rows", orgChanges)
	printChangeCounts("  role_rows", roleChanges)
	printChangeCounts("  user_rows", userChanges)
	printChangeCounts("  membership_rows", membershipChanges)
	printChangeCounts("  assignment_rows", assignmentChanges)
	printSamples(orgs)
	printChangeSummary("  planned_change_summary", changeDetails)
	printChangeDetails("  planned_change_details", changeDetails)
}

func printGlobalRolePlan(roles []workos.Role, changes changeCounts, details []changeDetail) {
	fmt.Println("Global role preflight:")
	fmt.Printf("  workos_global_roles: %d\n", len(roles))
	printChangeCounts("  role_rows", changes)
	for _, role := range sampleRoles(roles) {
		fmt.Printf("    %s (%s)\n", role.Slug, role.Name)
	}
	printChangeSummary("  planned_change_summary", details)
	printChangeDetails("  planned_change_details", details)
}

func printSamples(orgs []orgExpectation) {
	limit := min(len(orgs), sampleSize)
	if limit == 0 {
		return
	}
	fmt.Println("  sample:")
	for _, org := range orgs[:limit] {
		status := org.gramOrgID
		if org.skipped {
			status = "skip"
		}
		fmt.Printf("    %s -> %s org=%s roles=%s users=%s memberships=%s assignments=%s name=%q\n",
			org.workosOrgID,
			status,
			formatDominantChange(org.orgChanges),
			formatDominantChange(org.roleChanges),
			formatDominantChange(org.userChanges),
			formatDominantChange(org.membershipChanges),
			formatDominantChange(org.assignmentChanges),
			org.name,
		)
	}
	if len(orgs) > limit {
		fmt.Printf("    ... and %d more\n", len(orgs)-limit)
	}
}

func organizationSummary(orgs []orgExpectation) string {
	var skipped int
	for _, org := range orgs {
		if org.skipped {
			skipped++
		}
	}
	return fmt.Sprintf("apply %d planned row changes; skip %d unlinked WorkOS organizations without external_id", plannedOrganizationMutations(orgs), skipped)
}

func plannedOrganizationMutations(orgs []orgExpectation) int {
	var mutating int
	for _, org := range orgs {
		if !org.skipped {
			mutating += plannedOrganizationMutation(org)
		}
	}
	return mutating
}

func plannedOrganizationMutation(org orgExpectation) int {
	return org.orgChanges.Mutating() +
		org.roleChanges.Mutating() +
		org.userChanges.Mutating() +
		org.membershipChanges.Mutating() +
		org.assignmentChanges.Mutating()
}

func printChangeCounts(label string, counts changeCounts) {
	fmt.Printf("%s: affected=%d create=%d update=%d delete=%d noop=%d stale_skip=%d\n",
		label,
		counts.Mutating(),
		counts.Create,
		counts.Update,
		counts.Delete,
		counts.Noop,
		counts.StaleSkip,
	)
}

func printChangeSummary(label string, details []changeDetail) {
	if len(details) == 0 {
		return
	}
	groups := summarizeChangeDetails(details)
	fmt.Printf("%s: groups=%d changed_records=%d\n", label, len(groups), len(details))
	for _, group := range groups {
		fmt.Printf("    %s %s risk=%s fields=%s count=%d\n",
			group.Action,
			group.Entity,
			group.Risk,
			strings.Join(group.Fields, ","),
			group.Count,
		)
		for _, sample := range group.Samples {
			fmt.Printf("      sample %s\n", sample.ID)
			for _, field := range sampleSummaryFields(sample.Fields) {
				fmt.Printf("        %s: %q -> %q\n", field.Name, field.Before, field.After)
			}
		}
		if group.Count > len(group.Samples) {
			fmt.Printf("      ... and %d more\n", group.Count-len(group.Samples))
		}
	}
}

func summarizeChangeDetails(details []changeDetail) []changeSummaryGroup {
	groupsByKey := make(map[string]*changeSummaryGroup)
	for _, detail := range details {
		fields := changeFieldNames(detail.Fields)
		risk := changeRisk(detail.Action, fields)
		key := strings.Join([]string{risk, detail.Entity, detail.Action, strings.Join(fields, ",")}, "|")
		group, ok := groupsByKey[key]
		if !ok {
			group = &changeSummaryGroup{
				Entity:  detail.Entity,
				Action:  detail.Action,
				Risk:    risk,
				Fields:  fields,
				Count:   0,
				Samples: nil,
			}
			groupsByKey[key] = group
		}
		group.Count++
		if len(group.Samples) < changeSummarySampleLimit {
			group.Samples = append(group.Samples, detail)
		}
	}

	groups := make([]changeSummaryGroup, 0, len(groupsByKey))
	for _, group := range groupsByKey {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		leftRisk := changeRiskRank(groups[i].Risk)
		rightRisk := changeRiskRank(groups[j].Risk)
		if leftRisk != rightRisk {
			return leftRisk < rightRisk
		}
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		if groups[i].Entity != groups[j].Entity {
			return groups[i].Entity < groups[j].Entity
		}
		if groups[i].Action != groups[j].Action {
			return groups[i].Action < groups[j].Action
		}
		return strings.Join(groups[i].Fields, ",") < strings.Join(groups[j].Fields, ",")
	})
	return groups
}

func changeFieldNames(fields []fieldChange) []string {
	names := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := seen[field.Name]; ok {
			continue
		}
		seen[field.Name] = struct{}{}
		names = append(names, field.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return []string{"<none>"}
	}
	return names
}

func sampleSummaryFields(fields []fieldChange) []fieldChange {
	if len(fields) <= changeSummarySampleLimit {
		return fields
	}
	ranked := append([]fieldChange(nil), fields...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return changeFieldRiskRank(ranked[i].Name) < changeFieldRiskRank(ranked[j].Name)
	})
	return ranked[:changeSummarySampleLimit]
}

func changeRisk(action string, fields []string) string {
	if action == "delete" {
		return "critical"
	}
	risk := "metadata_only"
	for _, field := range fields {
		fieldRisk := changeFieldRisk(field)
		if changeRiskRank(fieldRisk) < changeRiskRank(risk) {
			risk = fieldRisk
		}
	}
	return risk
}

func changeFieldRisk(field string) string {
	switch field {
	case "deleted", "deleted_at", "disabled_at", "workos_deleted", "workos_deleted_at":
		return "critical"
	case "organization_id", "role_id", "user_id", "workos_id", "workos_membership_id", "workos_slug", "workos_user_id":
		return "identity"
	case "email", "id", "name", "slug", "workos_description", "workos_name":
		return "display"
	case "display_name", "photo_url":
		return "profile"
	case "workos_created_at", "workos_last_event_id", "workos_updated_at":
		return "metadata_only"
	default:
		return "normal"
	}
}

func changeFieldRiskRank(field string) int {
	return changeRiskRank(changeFieldRisk(field))
}

func changeRiskRank(risk string) int {
	switch risk {
	case "critical":
		return 0
	case "identity":
		return 1
	case "display":
		return 2
	case "profile":
		return 3
	case "normal":
		return 4
	case "metadata_only":
		return 5
	default:
		return 6
	}
}

func printChangeDetails(label string, details []changeDetail) {
	if len(details) == 0 {
		return
	}
	fmt.Printf("%s: showing=%d total=%d\n", label, min(len(details), updateDetailLimit), len(details))
	limit := min(len(details), updateDetailLimit)
	for _, detail := range details[:limit] {
		fmt.Printf("    %s %s %s\n", detail.Action, detail.Entity, detail.ID)
		for _, field := range detail.Fields {
			fmt.Printf("      %s: %q -> %q\n", field.Name, field.Before, field.After)
		}
	}
	if len(details) > limit {
		fmt.Printf("    ... and %d more changed records\n", len(details)-limit)
	}
}

func organizationCreateDetail(org workos.Organization, gramOrgID string) changeDetail {
	fields := []fieldChange{
		{Name: "id", Before: "<missing>", After: gramOrgID},
		{Name: "name", Before: "<missing>", After: org.Name},
		{Name: "slug", Before: "<missing>", After: "generated unique slug"},
		{Name: "workos_id", Before: "<missing>", After: org.ID},
	}
	if updatedAt, err := parseWorkOSTime(org.UpdatedAt); err == nil {
		fields = append(fields, fieldChange{Name: "workos_updated_at", Before: "<missing>", After: timeDisplay(updatedAt)})
	}
	return changeDetail{Entity: "organization", ID: org.ID, Action: "create", Fields: fields}
}

func roleCreateDetail(entity string, role workos.Role, updatedAt time.Time) changeDetail {
	fields := []fieldChange{
		{Name: "workos_slug", Before: "<missing>", After: role.Slug},
		{Name: "workos_name", Before: "<missing>", After: role.Name},
		{Name: "workos_description", Before: "<missing>", After: role.Description},
	}
	if createdAt, err := parseWorkOSTime(role.CreatedAt); err == nil {
		fields = append(fields, fieldChange{Name: "workos_created_at", Before: "<missing>", After: timeDisplay(createdAt)})
	}
	fields = append(fields, fieldChange{Name: "workos_updated_at", Before: "<missing>", After: timeDisplay(updatedAt)})
	return changeDetail{Entity: entity, ID: role.Slug, Action: "create", Fields: fields}
}

func userCreateDetail(user workos.User, gramUserID string, createdAt, updatedAt time.Time) changeDetail {
	return changeDetail{
		Entity: "user",
		ID:     user.ID,
		Action: "create",
		Fields: []fieldChange{
			{Name: "id", Before: "<missing>", After: gramUserID},
			{Name: "email", Before: "<missing>", After: user.Email},
			{Name: "display_name", Before: "<missing>", After: displayNameFromWorkOSUser(user)},
			{Name: "photo_url", Before: "<missing>", After: user.ProfilePictureURL},
			{Name: "workos_id", Before: "<missing>", After: user.ID},
			{Name: "workos_created_at", Before: "<missing>", After: timeDisplay(createdAt)},
			{Name: "workos_updated_at", Before: "<missing>", After: timeDisplay(updatedAt)},
		},
	}
}

func membershipCreateDetail(organizationID string, member workos.Member, gramUserID string, updatedAt time.Time) changeDetail {
	return changeDetail{
		Entity: "membership",
		ID:     member.ID,
		Action: "create",
		Fields: []fieldChange{
			{Name: "organization_id", Before: "<missing>", After: organizationID},
			{Name: "user_id", Before: "<missing>", After: gramUserID},
			{Name: "workos_user_id", Before: "<missing>", After: member.UserID},
			{Name: "workos_membership_id", Before: "<missing>", After: member.ID},
			{Name: "workos_updated_at", Before: "<missing>", After: timeDisplay(updatedAt)},
		},
	}
}

func roleAssignmentCreateDetail(organizationID string, member workos.Member, gramUserID string) changeDetail {
	return changeDetail{
		Entity: "role_assignment",
		ID:     member.ID,
		Action: "create",
		Fields: []fieldChange{
			{Name: "organization_id", Before: "<missing>", After: organizationID},
			{Name: "user_id", Before: "<missing>", After: gramUserID},
			{Name: "workos_membership_id", Before: "<missing>", After: member.ID},
			{Name: "workos_slug", Before: "<missing>", After: member.RoleSlug},
		},
	}
}

func appendFieldChange(fields []fieldChange, name, before, after string) []fieldChange {
	if before == after {
		return fields
	}
	return append(fields, fieldChange{Name: name, Before: before, After: after})
}

func pgTextDisplay(value pgtype.Text) string {
	if !value.Valid {
		return "<null>"
	}
	return value.String
}

func pgTimeDisplay(value pgtype.Timestamptz) string {
	if !value.Valid {
		return "<null>"
	}
	return timeDisplay(value.Time)
}

func timeDisplay(value time.Time) string {
	if value.IsZero() {
		return "<zero>"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boolDisplay(value bool) string {
	return fmt.Sprintf("%t", value)
}

func uuidDisplay(value pgtype.UUID) string {
	if !value.Valid {
		return "<null>"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", value.Bytes[0:4], value.Bytes[4:6], value.Bytes[6:8], value.Bytes[8:10], value.Bytes[10:16])
}

func formatDominantChange(counts changeCounts) string {
	switch {
	case counts.Create > 0:
		return fmt.Sprintf("create:%d", counts.Create)
	case counts.Update > 0:
		return fmt.Sprintf("update:%d", counts.Update)
	case counts.Delete > 0:
		return fmt.Sprintf("delete:%d", counts.Delete)
	case counts.StaleSkip > 0:
		return fmt.Sprintf("stale_skip:%d", counts.StaleSkip)
	default:
		return fmt.Sprintf("noop:%d", counts.Noop)
	}
}

func printReport(title string, rep report) {
	fmt.Println(title)
	fmt.Printf("  scanned: %d\n", rep.scanned)
	fmt.Printf("  written: %d\n", rep.written)
	fmt.Printf("  validated: %d\n", rep.validated)
	fmt.Printf("  skipped: %d\n", rep.skipped)
	fmt.Printf("  skipped_noop: %d\n", rep.skippedNoop)
	fmt.Printf("  failed: %d\n", rep.failed)
	fmt.Printf("  validation_failures: %d\n", rep.validationFailures)
	if reportHasRowOutcomes(rep) {
		printChangeCounts("  organization_rows", rep.organizationRows)
		printChangeCounts("  role_rows", rep.roleRows)
		printChangeCounts("  user_rows", rep.userRows)
		printChangeCounts("  membership_rows", rep.membershipRows)
		printChangeCounts("  assignment_rows", rep.assignmentRows)
	}
}

func reportHasRowOutcomes(rep report) bool {
	return rep.organizationRows != (changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}) ||
		rep.roleRows != (changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}) ||
		rep.userRows != (changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}) ||
		rep.membershipRows != (changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0}) ||
		rep.assignmentRows != (changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0})
}

func confirmWrite(opts options, summary string) error {
	if opts.dryRun || opts.phase == phasePreflight || opts.phase == phaseValidate {
		return nil
	}
	fmt.Printf("Write preflight: %s\n", summary)
	fmt.Println("  DB changes run with lock_timeout=5s and statement_timeout=5min.")
	fmt.Println("  WorkOS snapshot writes are not automatically reversible.")
	if opts.autoApprove && opts.environment != envProd {
		return nil
	}
	return promptExact("Type backfill to continue: ", "backfill")
}

func confirmProdAccess(opts options) error {
	if opts.confirmProd == "prod" {
		return nil
	}
	if !term.IsTerminal(syscall.Stdin) || !term.IsTerminal(syscall.Stdout) {
		return errors.New("prod access requires --confirm-prod=prod in non-interactive mode")
	}
	return promptExact("You are connecting to prod. Type prod to continue: ", "prod")
}

func promptExact(prompt, want string) error {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	got, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(got) != want {
		return fmt.Errorf("confirmation did not match %q", want)
	}
	return nil
}

func waitForEnter(message string) {
	fmt.Println(message)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func sampleRoles(roles []workos.Role) []workos.Role {
	if len(roles) <= sampleSize {
		return roles
	}
	return roles[:sampleSize]
}

func set(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if item != "" {
			out[item] = true
		}
	}
	return out
}

func difference(left, right map[string]bool) []string {
	out := make([]string, 0)
	for item := range left {
		if !right[item] {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func pgTextEmptyEqual(value pgtype.Text, want string) bool {
	if want == "" {
		return !value.Valid
	}
	return value.Valid && value.String == want
}

func pgTimeEqual(value pgtype.Timestamptz, want time.Time) bool {
	return value.Valid && value.Time.Equal(want)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "workos-backfill: %v\n", err)
		flag.Usage()
		os.Exit(2)
	}
}
