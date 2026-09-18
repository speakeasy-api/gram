package identityproviderconnections

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SyncApplications records a sync request so the coordinator's next pass runs
// the connection, and nudges the coordinator.
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

	before, err := s.lock(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
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
	oktaRow, err := q.RequestOktaApplicationsSync(ctx, repo.RequestOktaApplicationsSyncParams{
		IdentityProviderConnectionID: id,
		OrganizationID:               authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record applications sync request").LogError(ctx, logger)
	}
	after := connectionRows{Connection: before.Connection, Okta: oktaRow, Managed: before.Managed}
	if err := s.audit.LogIdentityProviderConnectionSyncApplications(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log applications sync request").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit applications sync request").LogError(ctx, logger)
	}

	s.kickApplicationSync(ctx, logger)
	return buildConnectionView(after), nil
}

// kickApplicationSync nudges the coordinator off the request path; the
// schedule picks the connection up within its interval even when the nudge
// fails or is not configured.
func (s *Service) kickApplicationSync(ctx context.Context, logger *slog.Logger) {
	if s.syncTrigger == nil {
		logger.WarnContext(ctx, "applications sync trigger not configured; waiting for the scheduled pass")
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		triggerCtx, cancel := context.WithTimeout(detached, 10*time.Second)
		defer cancel()
		if err := s.syncTrigger.TriggerApplicationSync(triggerCtx); err != nil {
			logger.WarnContext(triggerCtx, "applications sync trigger failed; waiting for the scheduled pass", attr.SlogError(err))
		}
	}()
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
	if rows.Connection.Status != StatusVerified {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "the connection is not verified; the snapshot is only served for verified connections")
	}

	apps, err := oktaapplications.ListSnapshot(ctx, s.db, authCtx.ActiveOrganizationID, id, payload.IncludeRemoved, listApplicationsLimit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list applications snapshot").LogError(ctx, logger)
	}
	items := make([]*gen.IdentityProviderConnectionApplication, 0, len(apps))
	for _, app := range apps {
		items = append(items, buildApplicationView(app))
	}

	run, err := oktaapplications.LatestRun(ctx, s.db, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load last reconcile run").LogError(ctx, logger)
	}
	var lastRun *gen.IdentityProviderConnectionReconcileRun
	if run != nil {
		lastRun = buildReconcileRunView(*run)
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
