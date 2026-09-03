package agentmanagementgrants

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Mode string

const (
	ModeDryRun Mode = "dry-run"
	ModeApply  Mode = "apply"
	ModeVerify Mode = "verify"
)

var ErrVerificationFailed = errors.New("agent management grant verification failed")

var requiredScopes = []string{
	"agent:read",
	"agent:write",
	"agent:authorize",
	"agent:transfer",
}

const canonicalSelector = `{"resource_kind":"agent","resource_id":"*"}`

type Options struct {
	BatchSize        int
	SampleLimit      int
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

type DefectSample struct {
	OrganizationID string `json:"organization_id"`
	PrincipalURN   string `json:"principal_urn,omitempty"`
	Scope          string `json:"scope,omitempty"`
}

type Verification struct {
	TotalOrganizations        int64          `json:"total_organizations"`
	TargetAdminRoles          int64          `json:"target_admin_roles"`
	MissingAdminRoles         int64          `json:"missing_admin_roles"`
	MissingAdminRoleSamples   []DefectSample `json:"missing_admin_role_samples"`
	MissingRequiredGrants     int64          `json:"missing_required_grants"`
	MissingGrantOrganizations int64          `json:"missing_grant_organizations"`
	MissingGrantSamples       []DefectSample `json:"missing_grant_samples"`
	UnexpectedAgentGrants     int64          `json:"unexpected_agent_grants"`
	UnexpectedOrganizations   int64          `json:"unexpected_organizations"`
	UnexpectedGrantSamples    []DefectSample `json:"unexpected_grant_samples"`
	ReadyForEnforcement       bool           `json:"ready_for_enforcement"`
}

type Summary struct {
	Mode                 Mode         `json:"mode"`
	OrganizationsScanned int64        `json:"organizations_scanned"`
	GrantRowsChanged     int64        `json:"grant_rows_changed"`
	Batches              int64        `json:"batches"`
	LastCursor           string       `json:"last_cursor,omitempty"`
	Verification         Verification `json:"verification"`
}

type Runner struct {
	pool       *pgxpool.Pool
	options    Options
	afterBatch func() error
}

func NewRunner(pool *pgxpool.Pool, options Options) *Runner {
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.SampleLimit <= 0 {
		options.SampleLimit = 20
	}
	if options.SampleLimit > 100 {
		options.SampleLimit = 100
	}
	if options.LockTimeout < time.Millisecond {
		options.LockTimeout = 2 * time.Second
	}
	if options.StatementTimeout < time.Millisecond {
		options.StatementTimeout = 30 * time.Second
	}
	return &Runner{pool: pool, options: options, afterBatch: nil}
}

func (r *Runner) Run(ctx context.Context, mode Mode) (Summary, error) {
	var emptyVerification Verification
	summary := Summary{
		Mode: mode, OrganizationsScanned: 0, GrantRowsChanged: 0, Batches: 0,
		LastCursor: "", Verification: emptyVerification,
	}
	switch mode {
	case ModeDryRun, ModeVerify:
		verification, err := r.Verify(ctx)
		summary.OrganizationsScanned = verification.TotalOrganizations
		summary.Verification = verification
		if err != nil {
			return summary, err
		}
		if mode == ModeVerify && !verification.ReadyForEnforcement {
			return summary, ErrVerificationFailed
		}
		return summary, nil
	case ModeApply:
	default:
		return summary, fmt.Errorf("unsupported migration mode %q", mode)
	}

	for {
		batch, changed, err := r.applyBatch(ctx, summary.LastCursor)
		if err != nil {
			return summary, err
		}
		if len(batch) == 0 {
			break
		}
		organizations := countOrganizations(batch)
		summary.OrganizationsScanned += int64(organizations)
		summary.GrantRowsChanged += changed
		summary.Batches++
		summary.LastCursor = batch[len(batch)-1].organizationID
		if r.afterBatch != nil {
			if err := r.afterBatch(); err != nil {
				return summary, fmt.Errorf("after committed agent management grant batch: %w", err)
			}
		}
		if organizations < r.options.BatchSize {
			break
		}
	}

	verification, err := r.Verify(ctx)
	summary.Verification = verification
	summary.OrganizationsScanned = verification.TotalOrganizations
	if err != nil {
		return summary, err
	}
	if !verification.ReadyForEnforcement {
		return summary, ErrVerificationFailed
	}
	return summary, nil
}

type adminRoleTarget struct {
	organizationID string
	principalURN   string
}

func (r *Runner) applyBatch(ctx context.Context, afterOrganizationID string) ([]adminRoleTarget, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: "", AccessMode: "", DeferrableMode: "", BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("begin agent management grant batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.configureTransaction(ctx, tx); err != nil {
		return nil, 0, fmt.Errorf("set agent management grant batch timeouts: %w", err)
	}

	rows, err := tx.Query(ctx, targetAdminRolesCTE+`,
batch_organizations AS (
  SELECT DISTINCT organization_id
  FROM target_admin_roles
  WHERE organization_id > $1
  ORDER BY organization_id
  LIMIT $2
)
SELECT target.organization_id, target.principal_urn
FROM target_admin_roles AS target
JOIN batch_organizations AS batch USING (organization_id)
ORDER BY target.organization_id, target.principal_urn`, afterOrganizationID, r.options.BatchSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list administrator role batch: %w", err)
	}
	targets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminRoleTarget, error) {
		var target adminRoleTarget
		if err := row.Scan(&target.organizationID, &target.principalURN); err != nil {
			return target, fmt.Errorf("scan administrator role target: %w", err)
		}
		return target, nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read administrator role batch: %w", err)
	}
	if len(targets) == 0 {
		return nil, 0, nil
	}

	organizationIDs := make([]string, 0, len(targets))
	principalURNs := make([]string, 0, len(targets))
	for _, target := range targets {
		organizationIDs = append(organizationIDs, target.organizationID)
		principalURNs = append(principalURNs, target.principalURN)
	}

	result, err := tx.Exec(ctx, `
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
SELECT target.organization_id, target.principal_urn, required.scope, $4::jsonb
FROM unnest($1::text[], $2::text[]) AS target(organization_id, principal_urn)
CROSS JOIN unnest($3::text[]) AS required(scope)
ON CONFLICT (organization_id, principal_urn, scope, selectors)
DO UPDATE SET
  effect = NULL,
  updated_at = clock_timestamp()
WHERE principal_grants.effect IS NOT NULL`, organizationIDs, principalURNs, requiredScopes, canonicalSelector)
	if err != nil {
		return nil, 0, fmt.Errorf("backfill agent management grants: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit agent management grant batch: %w", err)
	}
	return targets, result.RowsAffected(), nil
}

func (r *Runner) Verify(ctx context.Context) (Verification, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return Verification{}, fmt.Errorf("begin agent management grant verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := r.configureTransaction(ctx, tx); err != nil {
		return Verification{}, err
	}

	verification := Verification{
		TotalOrganizations: 0, TargetAdminRoles: 0, MissingAdminRoles: 0, MissingAdminRoleSamples: nil,
		MissingRequiredGrants: 0, MissingGrantOrganizations: 0, MissingGrantSamples: nil,
		UnexpectedAgentGrants: 0, UnexpectedOrganizations: 0, UnexpectedGrantSamples: nil, ReadyForEnforcement: false,
	}
	if err := tx.QueryRow(ctx, targetAdminRolesCTE+`
SELECT
  (SELECT COUNT(*) FROM organization_metadata),
  (SELECT COUNT(*) FROM target_admin_roles),
  (SELECT COUNT(*) FROM organization_metadata) - (SELECT COUNT(DISTINCT organization_id) FROM target_admin_roles)`).Scan(
		&verification.TotalOrganizations, &verification.TargetAdminRoles, &verification.MissingAdminRoles,
	); err != nil {
		return Verification{}, fmt.Errorf("count administrator role targets: %w", err)
	}

	missingAdminRows, err := tx.Query(ctx, targetAdminRolesCTE+`
SELECT om.id
FROM organization_metadata AS om
LEFT JOIN target_admin_roles AS target ON target.organization_id = om.id
WHERE target.organization_id IS NULL
ORDER BY om.id
LIMIT $1`, r.options.SampleLimit)
	if err != nil {
		return Verification{}, fmt.Errorf("sample missing administrator roles: %w", err)
	}
	missingAdminIDs, err := pgx.CollectRows(missingAdminRows, pgx.RowTo[string])
	if err != nil {
		return Verification{}, fmt.Errorf("read missing administrator role samples: %w", err)
	}
	verification.MissingAdminRoleSamples = make([]DefectSample, 0, len(missingAdminIDs))
	for _, organizationID := range missingAdminIDs {
		verification.MissingAdminRoleSamples = append(verification.MissingAdminRoleSamples, DefectSample{
			OrganizationID: organizationID, PrincipalURN: "", Scope: "",
		})
	}

	if err := tx.QueryRow(ctx, missingRequiredGrantsCTE+`
SELECT COUNT(*), COUNT(DISTINCT organization_id)
FROM missing_required_grants`, requiredScopes, canonicalSelector).Scan(&verification.MissingRequiredGrants, &verification.MissingGrantOrganizations); err != nil {
		return Verification{}, fmt.Errorf("count missing agent management grants: %w", err)
	}
	missingRows, err := tx.Query(ctx, missingRequiredGrantsCTE+`
SELECT organization_id, principal_urn, scope
FROM missing_required_grants
ORDER BY organization_id, principal_urn, scope
LIMIT $3`, requiredScopes, canonicalSelector, r.options.SampleLimit)
	if err != nil {
		return Verification{}, fmt.Errorf("sample missing agent management grants: %w", err)
	}
	verification.MissingGrantSamples, err = collectDefectSamples(missingRows)
	if err != nil {
		return Verification{}, fmt.Errorf("read missing agent management grant samples: %w", err)
	}

	if err := tx.QueryRow(ctx, unexpectedAgentGrantsCTE+`
SELECT COUNT(*), COUNT(DISTINCT organization_id)
FROM unexpected_agent_grants`, requiredScopes, canonicalSelector).Scan(&verification.UnexpectedAgentGrants, &verification.UnexpectedOrganizations); err != nil {
		return Verification{}, fmt.Errorf("count unexpected agent management grants: %w", err)
	}
	unexpectedRows, err := tx.Query(ctx, unexpectedAgentGrantsCTE+`
SELECT organization_id, principal_urn, scope
FROM unexpected_agent_grants
ORDER BY organization_id, principal_urn, scope
LIMIT $3`, requiredScopes, canonicalSelector, r.options.SampleLimit)
	if err != nil {
		return Verification{}, fmt.Errorf("sample unexpected agent management grants: %w", err)
	}
	verification.UnexpectedGrantSamples, err = collectDefectSamples(unexpectedRows)
	if err != nil {
		return Verification{}, fmt.Errorf("read unexpected agent management grant samples: %w", err)
	}

	verification.ReadyForEnforcement = verification.MissingAdminRoles == 0 &&
		verification.MissingRequiredGrants == 0 && verification.UnexpectedAgentGrants == 0
	if err := tx.Commit(ctx); err != nil {
		return Verification{}, fmt.Errorf("commit agent management grant verification: %w", err)
	}
	return verification, nil
}

func (r *Runner) configureTransaction(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)`,
		strconv.FormatInt(r.options.LockTimeout.Milliseconds(), 10)+"ms",
		strconv.FormatInt(r.options.StatementTimeout.Milliseconds(), 10)+"ms")
	if err != nil {
		return fmt.Errorf("set agent management grant transaction timeouts: %w", err)
	}
	return nil
}

func countOrganizations(targets []adminRoleTarget) int {
	count := 0
	previous := ""
	for _, target := range targets {
		if count == 0 || target.organizationID != previous {
			count++
			previous = target.organizationID
		}
	}
	return count
}

func collectDefectSamples(rows pgx.Rows) ([]DefectSample, error) {
	samples, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DefectSample, error) {
		var sample DefectSample
		if err := row.Scan(&sample.OrganizationID, &sample.PrincipalURN, &sample.Scope); err != nil {
			return sample, fmt.Errorf("scan agent management grant defect: %w", err)
		}
		return sample, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect agent management grant defects: %w", err)
	}
	return samples, nil
}

const targetAdminRolesCTE = `
WITH target_admin_roles AS (
  SELECT
    organization.id AS organization_id,
    'role:global:' || global_role.id::text AS principal_urn
  FROM organization_metadata AS organization
  JOIN global_roles AS global_role
    ON global_role.workos_slug = 'admin'
    AND global_role.deleted IS FALSE
    AND global_role.workos_deleted IS FALSE
  UNION ALL
  SELECT
    organization.id AS organization_id,
    'role:organization:' || organization_role.id::text AS principal_urn
  FROM organization_metadata AS organization
  JOIN organization_roles AS organization_role
    ON organization_role.organization_id = organization.id
    AND organization_role.workos_slug = 'admin'
    AND organization_role.deleted IS FALSE
    AND organization_role.workos_deleted IS FALSE
)`

const missingRequiredGrantsCTE = targetAdminRolesCTE + `,
required_scopes AS (
  SELECT unnest($1::text[]) AS scope
),
missing_required_grants AS (
  SELECT target.organization_id, target.principal_urn, required.scope
  FROM target_admin_roles AS target
  CROSS JOIN required_scopes AS required
  LEFT JOIN principal_grants AS principal_grant
    ON principal_grant.organization_id = target.organization_id
    AND principal_grant.principal_urn = target.principal_urn
    AND principal_grant.scope = required.scope
    AND COALESCE(principal_grant.effect, 'allow') = 'allow'
    AND principal_grant.selectors = $2::jsonb
  WHERE principal_grant.id IS NULL
)`

const unexpectedAgentGrantsCTE = targetAdminRolesCTE + `,
unexpected_agent_grants AS (
  SELECT principal_grant.organization_id, principal_grant.principal_urn, principal_grant.scope
  FROM target_admin_roles AS target
  JOIN principal_grants AS principal_grant
    ON principal_grant.organization_id = target.organization_id
    AND principal_grant.principal_urn = target.principal_urn
  WHERE principal_grant.scope LIKE 'agent:%'
    AND NOT (
      principal_grant.scope = ANY($1::text[])
      AND COALESCE(principal_grant.effect, 'allow') = 'allow'
      AND principal_grant.selectors = $2::jsonb
    )
)`
