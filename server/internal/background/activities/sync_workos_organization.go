package activities

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"go.temporal.io/sdk/temporal"
)

var errSyncWorkOSOrganizationLockTimeout = errors.New("timed out waiting for lock to sync WorkOS organization")

type SyncWorkOSOrganization struct {
	db     *pgxpool.Pool
	logger *slog.Logger
	client *workos.Client
}

func NewSyncWorkOSOrg(logger *slog.Logger, db *pgxpool.Pool, client *workos.Client) *SyncWorkOSOrganization {
	return &SyncWorkOSOrganization{
		db:     db,
		logger: logger,
		client: client,
	}
}

type SyncWorkOSOrgResult struct {
	UpdateWorkOSOrgID bool
	RolesSynced       int
	RolesFailed       int
}

func (s *SyncWorkOSOrganization) Do(ctx context.Context, gramOrgID string) (*SyncWorkOSOrgResult, error) {
	res, err := s.do(ctx, gramOrgID)
	if err != nil {
		nonRetryable := false
		var se *oops.ShareableError
		switch {
		case errors.As(err, &se):
			nonRetryable = !se.IsTemporary()
		default:
			nonRetryable = errors.Is(err, oops.ErrPermanent)
		}

		return nil, temporal.NewApplicationErrorWithOptions("workos sync failed for organization", "workos_sync_error", temporal.ApplicationErrorOptions{
			NonRetryable: nonRetryable,
			Cause:        err,
		})
	}

	return res, nil
}

func (s *SyncWorkOSOrganization) do(ctx context.Context, gramOrgID string) (*SyncWorkOSOrgResult, error) {
	logger := s.logger.With(attr.SlogOrganizationID(gramOrgID))

	orgmeta, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization metadata").Log(ctx, logger)
	}
	logger = logger.With(
		attr.SlogOrganizationSlug(orgmeta.Slug),
		attr.SlogWorkOSOrganizationID(orgmeta.WorkosID.String),
		attr.SlogOrganizationAccountType(orgmeta.GramAccountType),
	)

	workosState, err := s.fetchWorkOSState(ctx, logger, orgmeta)
	if err != nil {
		return nil, err
	}

	// 👆 Fetch all state from WorkOS before opening database transaction. We do
	// do not want to hold a transaction open while waiting for potentially slow
	// HTTP requests. When we have everything we need, we access the database
	// and update the relevant tables with the fetched state.

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin workos sync").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	dbr := database.New(dbtx)
	// We only want one sync for a given organization to run at a time, so we
	// acquire an advisory lock for the duration of the sync.
	lockID, err := database.LockIDForSyncWorkOSOrganization(gramOrgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate workos sync advisory lock ID").Log(ctx, logger)
	}
	lockWaitCtx, cancel := context.WithTimeoutCause(ctx, 10*time.Second, errSyncWorkOSOrganizationLockTimeout)
	defer cancel()
	if err := dbr.ObtainExclusiveTxAdvisoryLock(lockWaitCtx, lockID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to obtain exclusive lock for workos sync").Log(ctx, logger)
	}

	if workosState.StoreWorkOSOrganizationID {
		if err := s.syncOrg(ctx, logger, dbtx, gramOrgID, workosState.WorkOSOrganizationID); err != nil {
			return nil, err
		}
	}

	roleResult, err := s.syncRoles(ctx, logger, dbtx, gramOrgID, workosState.Roles)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to commit workos sync").Log(ctx, logger)
	}

	return &SyncWorkOSOrgResult{
		UpdateWorkOSOrgID: workosState.StoreWorkOSOrganizationID,
		RolesSynced:       roleResult.successes,
		RolesFailed:       roleResult.failures,
	}, nil
}

type workosState struct {
	WorkOSOrganizationID      string
	StoreWorkOSOrganizationID bool
	Roles                     []workos.Role
}

func (s *SyncWorkOSOrganization) fetchWorkOSState(ctx context.Context, logger *slog.Logger, org orgrepo.OrganizationMetadatum) (workosState, error) {
	storeWorkOSOrgID := false
	workosOrgID := conv.PtrValOrEmpty(conv.FromPGText[string](org.WorkosID), "")
	if workosOrgID == "" {
		worg, err := s.client.GetOrganizationByGramID(ctx, org.ID)
		if err == nil {
			workosOrgID = worg.ID
			storeWorkOSOrgID = true
		} else {
			return workosState{}, oops.E(oops.CodeUnexpected, oops.ErrPermanent, "failed to get WorkOS organization with gram id").Log(ctx, logger)
		}
	}

	roles, err := s.client.ListRoles(ctx, workosOrgID)
	if err != nil {
		return workosState{}, oops.E(oops.CodeUnexpected, err, "failed to list roles").Log(ctx, logger)
	}

	return workosState{
		WorkOSOrganizationID:      workosOrgID,
		StoreWorkOSOrganizationID: storeWorkOSOrgID,
		Roles:                     roles,
	}, nil
}

func (s *SyncWorkOSOrganization) syncOrg(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, gramOrgID string, workosOrgID string) error {
	orgr := orgrepo.New(dbtx)
	if _, err := orgr.SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{
		OrganizationID: gramOrgID,
		WorkosID:       conv.ToPGTextEmpty(workosOrgID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to store WorkOS organization ID").Log(ctx, logger)
	}

	return nil
}

type syncRoleResult struct {
	successes int
	failures  int
}

func (s *SyncWorkOSOrganization) syncRoles(ctx context.Context, logger *slog.Logger, dbtx database.DBTX, gramOrgID string, roles []workos.Role) (*syncRoleResult, error) {
	accessr := accessrepo.New(dbtx)

	totalRoles := len(roles)
	failedRoles := 0
	for _, role := range roles {
		if err := accessr.UpsertRole(ctx, accessrepo.UpsertRoleParams{
			OrganizationID:    gramOrgID,
			WorkosID:          role.ID,
			WorkosSlug:        role.Slug,
			WorkosName:        role.Name,
			WorkosDescription: conv.ToPGTextEmpty(role.Description),
			WorkosCreatedAt:   pgtype.Timestamptz{Time: role.CreatedAt, InfinityModifier: 0, Valid: true},
			WorkosUpdatedAt:   pgtype.Timestamptz{Time: role.UpdatedAt, InfinityModifier: 0, Valid: true},
		}); err != nil {
			logger.ErrorContext(ctx, "failed to upsert role", attr.SlogRoleWorkOSID(role.ID), attr.SlogRoleWorkOSSlug(role.Slug))
			continue
		}
	}

	successes := totalRoles - failedRoles

	return &syncRoleResult{successes: successes, failures: failedRoles}, nil
}
