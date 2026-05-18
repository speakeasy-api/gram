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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/term"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const sampleSize = 5
const updateDetailLimit = 20

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

	databaseURL := opts.databaseURL
	var cleanupCloudSQLProxy func()
	if opts.cloudSQLProxy {
		var err error
		databaseURL, cleanupCloudSQLProxy, err = startCloudSQLProxy(ctx, opts)
		if err != nil {
			return err
		}
		defer cleanupCloudSQLProxy()
	}

	readOnly := opts.dryRun || opts.phase == phasePreflight || opts.phase == phaseValidate
	db, err := connectDB(ctx, databaseURL, readOnly)
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
		return errors.New("backfill completed with validation failures")
	}
	return nil
}

func shouldValidate(opts options) bool {
	return opts.phase == phaseValidate || !opts.dryRun
}

func connectDB(ctx context.Context, databaseURL string, readOnly bool) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET lock_timeout = '5s'"); err != nil {
			return fmt.Errorf("set lock_timeout: %w", err)
		}
		if _, err := conn.Exec(ctx, "SET statement_timeout = '5min'"); err != nil {
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

	out := make([]orgExpectation, 0, len(workosOrgs))
	for _, org := range workosOrgs {
		gramOrgID, skipped, err := expectedGramOrgID(ctx, db, org)
		if err != nil {
			return nil, err
		}
		roles, err := workosClient.ListRoles(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list roles for %s: %w", org.ID, err)
		}
		users, err := workosClient.ListOrgUsers(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list users for %s: %w", org.ID, err)
		}
		members, err := workosClient.ListOrgMemberships(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list memberships for %s: %w", org.ID, err)
		}

		orgChanges, err := classifyOrganizationMetadataChange(ctx, db, org, gramOrgID, skipped)
		if err != nil {
			return nil, err
		}
		roleChanges, err := classifyOrganizationRoleChanges(ctx, db, gramOrgID, skipped, roles)
		if err != nil {
			return nil, err
		}
		userChanges, err := classifyUserChanges(ctx, db, skipped, users)
		if err != nil {
			return nil, err
		}
		membershipChanges, err := classifyMembershipChanges(ctx, db, gramOrgID, skipped, users, members)
		if err != nil {
			return nil, err
		}
		assignmentChanges, err := classifyAssignmentChanges(ctx, db, gramOrgID, skipped, users, members)
		if err != nil {
			return nil, err
		}
		changeDetails, err := collectOrganizationChangeDetails(ctx, db, org, gramOrgID, skipped, roles, users, members)
		if err != nil {
			return nil, err
		}

		out = append(out, orgExpectation{
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
		})
	}

	return out, nil
}

func selectedOrganizations(ctx context.Context, workosClient *workos.Client, opts options) ([]workos.Organization, error) {
	if len(opts.workosOrgIDs) > 0 {
		out := make([]workos.Organization, 0, len(opts.workosOrgIDs))
		for _, orgID := range opts.workosOrgIDs {
			org, err := workosClient.GetOrganization(ctx, orgID)
			if err != nil {
				return nil, fmt.Errorf("get WorkOS organization %s: %w", orgID, err)
			}
			out = append(out, *org)
		}
		return out, nil
	}

	orgs, err := workosClient.ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list WorkOS organizations: %w", err)
	}
	sort.Slice(orgs, func(i, j int) bool { return orgs[i].ID < orgs[j].ID })
	if opts.limit > 0 && len(orgs) > opts.limit {
		orgs = orgs[:opts.limit]
	}
	return orgs, nil
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
		return "create", nil
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
		return "create", nil
	case err != nil:
		return "", fmt.Errorf("query local membership %q: %w", member.ID, err)
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

func classifyAssignmentChanges(ctx context.Context, db *pgxpool.Pool, organizationID string, skipped bool, users map[string]workos.User, members []workos.Member) (changeCounts, error) {
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

	assignmentDetails, err := collectAssignmentChangeDetails(ctx, db, gramOrgID, users, members)
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
		existing, found, err := findUserByWorkOSID(ctx, db, user.ID)
		if err != nil {
			return nil, err
		}
		if !found || existing.WorkosUpdatedAt.Valid && !shouldProcessEvent(nil, &existing.WorkosUpdatedAt.Time, "", updatedAt) {
			continue
		}
		fields := make([]fieldChange, 0)
		fields = appendFieldChange(fields, "email", existing.Email, user.Email)
		fields = appendFieldChange(fields, "display_name", existing.DisplayName, displayNameFromWorkOSUser(user))
		if !pgTextEmptyEqual(existing.PhotoUrl, user.ProfilePictureURL) {
			fields = appendFieldChange(fields, "photo_url", pgTextDisplay(existing.PhotoUrl), user.ProfilePictureURL)
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
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, nil
	case err != nil:
		return changeDetail{Entity: "", ID: "", Action: "", Fields: nil}, false, fmt.Errorf("query local membership %q: %w", member.ID, err)
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

func collectAssignmentChangeDetails(ctx context.Context, db *pgxpool.Pool, organizationID string, users map[string]workos.User, members []workos.Member) ([]changeDetail, error) {
	details := make([]changeDetail, 0)
	for _, member := range members {
		gramUserID, resolved, err := expectedGramUserID(ctx, db, users[member.UserID])
		if err != nil {
			return nil, err
		}
		if !resolved {
			continue
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
		for rows.Next() {
			var id pgtype.UUID
			var userID pgtype.Text
			if err := rows.Scan(&id, &userID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan role assignment for membership %q: %w", member.ID, err)
			}
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
		} else if err := validateOrganization(ctx, db, org); err != nil {
			rep.validationFailures++
			fmt.Fprintf(os.Stderr, "  validation failed: %v\n", err)
		} else {
			rep.written++
			rep.validated++
			rep.organizationRows = rep.organizationRows.Add(org.orgChanges)
			rep.roleRows = rep.roleRows.Add(org.roleChanges)
			rep.userRows = rep.userRows.Add(org.userChanges)
			rep.membershipRows = rep.membershipRows.Add(org.membershipChanges)
			rep.assignmentRows = rep.assignmentRows.Add(org.assignmentChanges)
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
			roleMembershipIDs = append(roleMembershipIDs, member.ID)
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
	printChangeDetails("  planned_change_details", changeDetails)
}

func printGlobalRolePlan(roles []workos.Role, changes changeCounts, details []changeDetail) {
	fmt.Println("Global role preflight:")
	fmt.Printf("  workos_global_roles: %d\n", len(roles))
	printChangeCounts("  role_rows", changes)
	for _, role := range sampleRoles(roles) {
		fmt.Printf("    %s (%s)\n", role.Slug, role.Name)
	}
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
