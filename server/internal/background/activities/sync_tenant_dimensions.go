package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// tenantDimensionsReplaceLockTTL outlives one activity attempt (2m
// StartToClose) plus the longest ClickHouse statement that can still land
// afterwards: SELECT and INSERT pipelines are bounded by the connection's 60s
// max_execution_time, while DDL lock acquisition is bounded by the server's
// 120s lock_acquire_timeout. Revisit this TTL if either bound is raised.
const tenantDimensionsReplaceLockTTL = 5 * time.Minute

const tenantDimensionsReplaceLockKey = "tenant-dimensions:replace-lock"

type tenantDimensionsTelemetry interface {
	ReplaceTenantDimensions(ctx context.Context, organizations []telemetryrepo.OrganizationMetadataDimension, projects []telemetryrepo.ProjectDimension) error
}

// SyncTenantDimensions publishes current organization and project reporting
// dimensions from one repeatable-read Postgres snapshot into ClickHouse.
type SyncTenantDimensions struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	telemetry tenantDimensionsTelemetry
	cache     cache.Cache
}

// NewSyncTenantDimensions creates a tenant dimension synchronization activity.
func NewSyncTenantDimensions(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn, cacheAdapter cache.Cache) *SyncTenantDimensions {
	return &SyncTenantDimensions{
		logger:    logger.With(attr.SlogComponent("sync_tenant_dimensions")),
		db:        db,
		telemetry: telemetryrepo.New(chConn),
		cache:     cacheAdapter,
	}
}

// SyncTenantDimensionsResult describes one published ClickHouse generation.
type SyncTenantDimensionsResult struct {
	// Organizations is the number of organization dimension rows published.
	Organizations int

	// Projects is the number of project dimension rows published.
	Projects int

	// OrphanProjects is the number of projects whose organization was absent
	// from the same Postgres snapshot.
	OrphanProjects int
}

// Do reads and publishes one complete tenant dimension generation.
func (s *SyncTenantDimensions) Do(ctx context.Context) (*SyncTenantDimensionsResult, error) {
	leases, ok := s.cache.(cache.LeaseCache)
	if !ok {
		return nil, fmt.Errorf("tenant dimension cache does not support ownership-aware leases")
	}

	leaseOwner := uuid.NewString()
	claimed, err := leases.AcquireLease(ctx, tenantDimensionsReplaceLockKey, leaseOwner, tenantDimensionsReplaceLockTTL)
	if err != nil {
		return nil, fmt.Errorf("claim tenant dimension replacement lease: %w", err)
	}
	if !claimed {
		return nil, fmt.Errorf("tenant dimension replacement already in progress")
	}

	keepLeaseUntilExpiry := false
	defer func() {
		if keepLeaseUntilExpiry {
			return
		}

		released, err := leases.ReleaseLeaseIfOwner(context.WithoutCancel(ctx), tenantDimensionsReplaceLockKey, leaseOwner)
		switch {
		case err != nil:
			s.logger.WarnContext(ctx, "failed to release tenant dimension replacement lease", attr.SlogError(err))
		case !released:
			s.logger.WarnContext(ctx, "tenant dimension replacement lease expired before release")
		}
	}()

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.RepeatableRead,
		AccessMode:     pgx.ReadOnly,
		DeferrableMode: "",
		BeginQuery:     "",
		CommitQuery:    "",
	})
	if err != nil {
		return nil, fmt.Errorf("begin tenant dimension snapshot: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := activitiesrepo.New(tx)
	organizationRows, err := queries.ListTenantDimensionOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant dimension organizations: %w", err)
	}
	projectRows, err := queries.ListTenantDimensionProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenant dimension projects: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tenant dimension snapshot: %w", err)
	}

	organizations := make([]telemetryrepo.OrganizationMetadataDimension, 0, len(organizationRows))
	organizationIDs := make(map[string]struct{}, len(organizationRows))
	for _, row := range organizationRows {
		organization, err := organizationDimension(row)
		if err != nil {
			return nil, err
		}
		organizations = append(organizations, organization)
		organizationIDs[row.ID] = struct{}{}
	}

	projects := make([]telemetryrepo.ProjectDimension, 0, len(projectRows))
	orphanProjects := 0
	for _, row := range projectRows {
		project, err := projectDimension(row)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
		if _, exists := organizationIDs[row.OrganizationID]; !exists {
			orphanProjects++
		}
	}

	keepLeaseUntilExpiry = true

	if err := s.telemetry.ReplaceTenantDimensions(ctx, organizations, projects); err != nil {
		// Keep the lease until its TTL after a failed or ambiguous ClickHouse
		// statement so a delayed predecessor cannot overwrite a newer generation.
		return nil, fmt.Errorf("replace tenant dimensions: %w", err)
	}
	keepLeaseUntilExpiry = false

	result := &SyncTenantDimensionsResult{
		Organizations:  len(organizations),
		Projects:       len(projects),
		OrphanProjects: orphanProjects,
	}
	s.logger.InfoContext(ctx, "tenant dimensions synced",
		attr.SlogTenantDimensionOrganizationCount(result.Organizations),
		attr.SlogTenantDimensionProjectCount(result.Projects),
		attr.SlogTenantDimensionOrphanProjectCount(result.OrphanProjects),
	)
	return result, nil
}

func organizationDimension(row activitiesrepo.ListTenantDimensionOrganizationsRow) (telemetryrepo.OrganizationMetadataDimension, error) {
	freeTrialStartedAt, err := requiredTenantDimensionTime(row.FreeTrialStartedAt, "free_trial_started_at", row.ID)
	if err != nil {
		return telemetryrepo.OrganizationMetadataDimension{}, err
	}
	freeTrialEndsAt, err := requiredTenantDimensionTime(row.FreeTrialEndsAt, "free_trial_ends_at", row.ID)
	if err != nil {
		return telemetryrepo.OrganizationMetadataDimension{}, err
	}
	createdAt, err := requiredTenantDimensionTime(row.CreatedAt, "created_at", row.ID)
	if err != nil {
		return telemetryrepo.OrganizationMetadataDimension{}, err
	}
	updatedAt, err := requiredTenantDimensionTime(row.UpdatedAt, "updated_at", row.ID)
	if err != nil {
		return telemetryrepo.OrganizationMetadataDimension{}, err
	}

	return telemetryrepo.OrganizationMetadataDimension{
		ID:                 row.ID,
		Slug:               row.Slug,
		AccountType:        row.AccountType,
		WorkOSID:           conv.FromPGText[string](row.WorkosID),
		WorkOSUpdatedAt:    nullableTenantDimensionTime(row.WorkosUpdatedAt),
		WebhooksEnabled:    conv.FromPGBool[bool](row.WebhooksEnabled),
		SCIMEnabled:        conv.FromPGBool[bool](row.ScimEnabled),
		SSOEnabled:         conv.FromPGBool[bool](row.SsoEnabled),
		Whitelisted:        row.Whitelisted,
		FreeTrialStartedAt: freeTrialStartedAt,
		FreeTrialEndsAt:    freeTrialEndsAt,
		TrialTier:          conv.FromPGText[string](row.TrialTier),
		TrialEndsAt:        nullableTenantDimensionTime(row.TrialEndsAt),
		TrialConvertedAt:   nullableTenantDimensionTime(row.TrialConvertedAt),
		TrialDemotedAt:     nullableTenantDimensionTime(row.TrialDemotedAt),
		TrialCreatedAt:     nullableTenantDimensionTime(row.TrialCreatedAt),
		TrialUpdatedAt:     nullableTenantDimensionTime(row.TrialUpdatedAt),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		DisabledAt:         nullableTenantDimensionTime(row.DisabledAt),
	}, nil
}

func projectDimension(row activitiesrepo.ListTenantDimensionProjectsRow) (telemetryrepo.ProjectDimension, error) {
	createdAt, err := requiredTenantDimensionTime(row.CreatedAt, "created_at", row.ID.String())
	if err != nil {
		return telemetryrepo.ProjectDimension{}, err
	}
	updatedAt, err := requiredTenantDimensionTime(row.UpdatedAt, "updated_at", row.ID.String())
	if err != nil {
		return telemetryrepo.ProjectDimension{}, err
	}

	return telemetryrepo.ProjectDimension{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Slug:           row.Slug,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		DeletedAt:      nullableTenantDimensionTime(row.DeletedAt),
	}, nil
}

func requiredTenantDimensionTime(value pgtype.Timestamptz, field, id string) (time.Time, error) {
	if !value.Valid || value.InfinityModifier != pgtype.Finite {
		return time.Time{}, fmt.Errorf("tenant dimension %s has invalid %s", id, field)
	}
	return value.Time.UTC(), nil
}

func nullableTenantDimensionTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid || value.InfinityModifier != pgtype.Finite {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
