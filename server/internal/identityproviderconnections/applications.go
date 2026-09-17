package identityproviderconnections

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SyncApplications clears the snapshot watermark so the coordinator's next
// pass runs the connection, and nudges the coordinator.
func (s *Service) SyncApplications(ctx context.Context, payload *gen.SyncApplicationsPayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	before, err := s.lock(ctx, logger, q, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}
	if before.Connection.Status != StatusVerified {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "verify the connection before syncing applications")
	}
	// Only a run that will actually happen spends the limiter's budget.
	if err := s.allow(ctx, logger, s.syncLimiter, authCtx.ActiveOrganizationID, "applications sync rate limit exceeded, try again shortly"); err != nil {
		return nil, err
	}
	oktaRow, err := q.MarkOktaApplicationsSyncDue(ctx, repo.MarkOktaApplicationsSyncDueParams{
		IdentityProviderConnectionID: id,
		OrganizationID:               authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mark applications sync due").LogError(ctx, logger)
	}
	after := connectionRows{Connection: before.Connection, Okta: oktaRow, Managed: before.Managed}
	if err := s.audit.LogIdentityProviderConnectionSyncApplications(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log applications sync request").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit applications sync request").LogError(ctx, logger)
	}

	// The coordinator's schedule picks the connection up within its interval
	// even when the nudge fails.
	if s.syncTrigger == nil {
		logger.WarnContext(ctx, "applications sync trigger not configured; waiting for the scheduled pass")
	} else if err := s.syncTrigger.TriggerApplicationSync(ctx); err != nil {
		logger.WarnContext(ctx, "applications sync trigger failed; waiting for the scheduled pass", attr.SlogError(err))
	}
	return buildConnectionView(after), nil
}

// ListApplications returns the snapshot with live assignment counts and the last run.
func (s *Service) ListApplications(ctx context.Context, payload *gen.ListApplicationsPayload) (*gen.ListIdentityProviderConnectionApplicationsResult, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, false)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	rows, err := s.load(ctx, logger, authCtx.ActiveOrganizationID, uuid.NullUUID{UUID: id, Valid: true})
	if err != nil {
		return nil, err
	}

	q := apprepo.New(s.db)
	apps, err := q.ListApplications(ctx, apprepo.ListApplicationsParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: id,
		IncludeRemoved:               payload.IncludeRemoved,
		LimitCount:                   listApplicationsLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list applications snapshot").LogError(ctx, logger)
	}
	items := make([]*gen.IdentityProviderConnectionApplication, 0, len(apps))
	for _, app := range apps {
		items = append(items, buildApplicationView(app))
	}

	var lastRun *gen.IdentityProviderConnectionReconcileRun
	run, err := q.GetLatestReconcileRun(ctx, apprepo.GetLatestReconcileRunParams{
		OrganizationID:               authCtx.ActiveOrganizationID,
		IdentityProviderConnectionID: id,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load last reconcile run").LogError(ctx, logger)
	default:
		lastRun = buildReconcileRunView(run)
	}

	return &gen.ListIdentityProviderConnectionApplicationsResult{
		Applications: items,
		Sync:         buildApplicationsSyncView(rows.Okta),
		LastRun:      lastRun,
	}, nil
}

func buildApplicationView(app apprepo.ListApplicationsRow) *gen.IdentityProviderConnectionApplication {
	features := app.Features
	if features == nil {
		features = []string{}
	}
	return &gen.IdentityProviderConnectionApplication{
		OktaAppID:        app.OktaAppID,
		Label:            app.Label,
		Name:             app.Name,
		SignOnMode:       app.SignOnMode,
		Status:           app.Status,
		Features:         features,
		UserAssignments:  int(app.UserAssignments),
		GroupAssignments: int(app.GroupAssignments),
		FirstSeenAt:      conv.FromPGTimestamptz(app.FirstSeenAt),
		LastSeenAt:       conv.FromPGTimestamptz(app.LastSeenAt),
		RemovedAt:        conv.PtrEmpty(conv.FromPGTimestamptz(app.RemovedAt)),
	}
}

func buildReconcileRunView(run apprepo.OktaApplicationReconcileRun) *gen.IdentityProviderConnectionReconcileRun {
	skipped := run.SkippedAppIds
	if skipped == nil {
		skipped = []string{}
	}
	return &gen.IdentityProviderConnectionReconcileRun{
		ID:                  run.ID.String(),
		Status:              run.Status,
		StartedAt:           conv.FromPGTimestamptz(run.StartedAt),
		FinishedAt:          conv.PtrEmpty(conv.FromPGTimestamptz(run.FinishedAt)),
		ApplicationsSeen:    int(run.ApplicationsSeen),
		ApplicationsAdded:   int(run.ApplicationsAdded),
		ApplicationsRemoved: int(run.ApplicationsRemoved),
		AssignmentsAdded:    int(run.AssignmentsAdded),
		AssignmentsRemoved:  int(run.AssignmentsRemoved),
		SkippedAppIds:       skipped,
		Truncated:           run.Truncated,
		Error:               conv.FromPGText[string](run.Error),
	}
}
