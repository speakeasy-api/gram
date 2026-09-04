package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TenantDimensionInsertChunk bounds one INSERT's parameter count. Organization
// rows carry twenty columns, so a smaller chunk than the identity map keeps each
// statement comfortably below driver and server limits.
const TenantDimensionInsertChunk = 1000

// OrganizationMetadataDimension is one organization reporting row sourced from
// Postgres organization_metadata and its optional enterprise trial lifecycle.
type OrganizationMetadataDimension struct {
	// ID is the Gram organization identifier.
	ID string

	// Slug is the organization's current Gram slug.
	Slug string

	// AccountType is the Gram billing and entitlement tier.
	AccountType string

	// WorkOSID is the linked WorkOS organization identifier.
	WorkOSID *string

	// WorkOSUpdatedAt is the timestamp of the latest applied WorkOS update.
	WorkOSUpdatedAt *time.Time

	// WebhooksEnabled records whether outbound organization webhooks are enabled.
	WebhooksEnabled *bool

	// SCIMEnabled records whether SCIM directory sync is enabled.
	SCIMEnabled *bool

	// SSOEnabled records whether SSO is enabled.
	SSOEnabled *bool

	// Whitelisted records whether the organization is allowed to use Gram.
	Whitelisted bool

	// FreeTrialStartedAt begins the organization metadata free-trial window.
	FreeTrialStartedAt time.Time

	// FreeTrialEndsAt ends the organization metadata free-trial window.
	FreeTrialEndsAt time.Time

	// TrialTier is the optional enterprise trial tier.
	TrialTier *string

	// TrialEndsAt is the current enterprise trial end time.
	TrialEndsAt *time.Time

	// TrialConvertedAt records when the enterprise trial converted.
	TrialConvertedAt *time.Time

	// TrialDemotedAt records when the enterprise trial was demoted.
	TrialDemotedAt *time.Time

	// TrialCreatedAt records when the enterprise trial lifecycle was created.
	TrialCreatedAt *time.Time

	// TrialUpdatedAt records when the enterprise trial lifecycle was updated.
	TrialUpdatedAt *time.Time

	// CreatedAt records when the organization was created in Gram.
	CreatedAt time.Time

	// UpdatedAt records when the organization metadata was updated in Gram.
	UpdatedAt time.Time

	// DisabledAt records when the organization was disabled.
	DisabledAt *time.Time
}

// ProjectDimension is one project reporting row sourced from Postgres.
type ProjectDimension struct {
	// ID is the Gram project identifier.
	ID uuid.UUID

	// OrganizationID is the organization that owns the project.
	OrganizationID string

	// Slug is the project's current slug within its organization.
	Slug string

	// CreatedAt records when the project was created.
	CreatedAt time.Time

	// UpdatedAt records when the project was updated.
	UpdatedAt time.Time

	// DeletedAt records when the project was soft-deleted.
	DeletedAt *time.Time
}

// ReplaceTenantDimensions rebuilds both staging tables before publishing their
// complete generations. Each EXCHANGE is atomic, while the pair is necessarily
// sequential because ClickHouse does not provide a multi-table atomic exchange.
func (q *Queries) ReplaceTenantDimensions(ctx context.Context, organizations []OrganizationMetadataDimension, projects []ProjectDimension) error {
	if err := validateTenantDimensions(organizations, projects); err != nil {
		return err
	}

	if err := q.conn.Exec(ctx, "TRUNCATE TABLE organization_metadata_staging"); err != nil {
		return fmt.Errorf("truncate organization metadata staging: %w", err)
	}
	if err := q.conn.Exec(ctx, "TRUNCATE TABLE projects_staging"); err != nil {
		return fmt.Errorf("truncate projects staging: %w", err)
	}

	for start := 0; start < len(organizations); start += TenantDimensionInsertChunk {
		chunk := organizations[start:min(start+TenantDimensionInsertChunk, len(organizations))]
		args := make([]any, 0, len(chunk)*20)
		for _, row := range chunk {
			args = append(args,
				row.ID,
				row.Slug,
				row.AccountType,
				nullableValue(row.WorkOSID),
				nullableUnixMicro(row.WorkOSUpdatedAt),
				nullableValue(row.WebhooksEnabled),
				nullableValue(row.SCIMEnabled),
				nullableValue(row.SSOEnabled),
				row.Whitelisted,
				row.FreeTrialStartedAt.UnixMicro(),
				row.FreeTrialEndsAt.UnixMicro(),
				nullableValue(row.TrialTier),
				nullableUnixMicro(row.TrialEndsAt),
				nullableUnixMicro(row.TrialConvertedAt),
				nullableUnixMicro(row.TrialDemotedAt),
				nullableUnixMicro(row.TrialCreatedAt),
				nullableUnixMicro(row.TrialUpdatedAt),
				row.CreatedAt.UnixMicro(),
				row.UpdatedAt.UnixMicro(),
				nullableUnixMicro(row.DisabledAt),
			)
		}
		query := "INSERT INTO organization_metadata_staging (id, slug, account_type, workos_id, workos_updated_at, webhooks_enabled, scim_enabled, sso_enabled, whitelisted, free_trial_started_at, free_trial_ends_at, trial_tier, trial_ends_at, trial_converted_at, trial_demoted_at, trial_created_at, trial_updated_at, created_at, updated_at, disabled_at) VALUES " + tenantDimensionValuesClause(len(chunk), "(?, ?, ?, ?, fromUnixTimestamp64Micro(?), ?, ?, ?, ?, fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), ?, fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?))")
		if err := q.conn.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("insert organization metadata staging chunk: %w", err)
		}
	}

	for start := 0; start < len(projects); start += TenantDimensionInsertChunk {
		chunk := projects[start:min(start+TenantDimensionInsertChunk, len(projects))]
		args := make([]any, 0, len(chunk)*6)
		for _, row := range chunk {
			args = append(args, row.ID, row.OrganizationID, row.Slug, row.CreatedAt.UnixMicro(), row.UpdatedAt.UnixMicro(), nullableUnixMicro(row.DeletedAt))
		}
		query := "INSERT INTO projects_staging (id, organization_id, slug, created_at, updated_at, deleted_at) VALUES " + tenantDimensionValuesClause(len(chunk), "(?, ?, ?, fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?), fromUnixTimestamp64Micro(?))")
		if err := q.conn.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("insert projects staging chunk: %w", err)
		}
	}

	if err := q.conn.Exec(ctx, "EXCHANGE TABLES organization_metadata_staging AND organization_metadata"); err != nil {
		return fmt.Errorf("exchange organization metadata tables: %w", err)
	}
	if err := q.conn.Exec(ctx, "EXCHANGE TABLES projects_staging AND projects"); err != nil {
		return fmt.Errorf("exchange project tables: %w", err)
	}

	return nil
}

func validateTenantDimensions(organizations []OrganizationMetadataDimension, projects []ProjectDimension) error {
	organizationIDs := make(map[string]struct{}, len(organizations))
	for _, organization := range organizations {
		if _, exists := organizationIDs[organization.ID]; exists {
			return fmt.Errorf("duplicate organization dimension id %q", organization.ID)
		}
		organizationIDs[organization.ID] = struct{}{}
	}

	projectIDs := make(map[uuid.UUID]struct{}, len(projects))
	for _, project := range projects {
		if _, exists := projectIDs[project.ID]; exists {
			return fmt.Errorf("duplicate project dimension id %q", project.ID)
		}
		projectIDs[project.ID] = struct{}{}
	}

	return nil
}

func tenantDimensionValuesClause(rows int, tuple string) string {
	values := make([]string, rows)
	for i := range values {
		values[i] = tuple
	}
	return strings.Join(values, ", ")
}

func nullableValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableUnixMicro(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMicro()
}
