package oktaapplications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

const (
	// SyncInterval is the platform-wide cadence; a connection is due once its
	// last run started this long ago or a request arrived after it.
	SyncInterval = 6 * time.Hour

	// MaxAttempts is how many times Temporal runs one sync before giving up;
	// the last attempt advances the watermark so a failing tenant waits a
	// full interval instead of being re-listed every coordinator pass.
	MaxAttempts = 3

	// maxApplicationsPerRun caps the snapshot; larger tenants get a truncated
	// run rather than an unbounded one.
	maxApplicationsPerRun = 2000

	// maxAssignmentsPerRun caps the assignments held and written by one run;
	// apps past the cap are recorded with incomplete assignments.
	maxAssignmentsPerRun = 100000

	// listPageSize is the Okta page size for every listing.
	listPageSize = 200

	// maxApplicationPages stops the application listing one page past the
	// cap; maxAssignmentPages bounds each per-app listing.
	maxApplicationPages = maxApplicationsPerRun/listPageSize + 1
	maxAssignmentPages  = 200

	// oktaCallTimeout bounds one listing (all of its pages).
	oktaCallTimeout = 5 * time.Minute

	// upsertChunk is how many rows one bulk statement carries.
	upsertChunk = 500

	// runRetention bounds the reconcile run history per connection.
	runRetention = 30 * 24 * time.Hour

	// appStatusActive is Okta's status for an enabled application.
	appStatusActive = "ACTIVE"
)

var (
	errClientUnavailable = errors.New("okta applications: client factory not configured")
	errConnectionGone    = errors.New("okta applications: connection no longer verified")
	errSuperseded        = errors.New("okta applications: a later run already applied")
)

// retryableError marks a failure Temporal should retry: our own infrastructure,
// an Okta rate limit, or an Okta outage. Everything else is recorded on the
// run and returns nil.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err}
}

// IsRetryable reports whether Run wants another attempt.
func IsRetryable(err error) bool {
	var re *retryableError
	return errors.As(err, &re)
}

// SyncCandidate is one due connection the coordinator should run.
type SyncCandidate struct {
	ConnectionID     uuid.UUID
	OrganizationID   string
	OrganizationSlug string
}

// Syncer executes application snapshot runs. Credentials are resolved inside
// Run from the connection id; Temporal payloads carry the id only.
type Syncer struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	repo    *repo.Queries
	clients okta.ClientFactory
	metrics *syncMetrics
}

// NewSyncer builds a Syncer. A nil client factory records every run as failed.
func NewSyncer(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, clients okta.ClientFactory) *Syncer {
	logger = logger.With(attr.SlogComponent("oktaapplications.sync"))
	return &Syncer{
		logger:  logger,
		db:      db,
		repo:    repo.New(db),
		clients: clients,
		metrics: newSyncMetrics(logger, meterProvider),
	}
}

// ListCandidates returns due verified connections, excluding ids already
// attempted this pass.
func (s *Syncer) ListCandidates(ctx context.Context, limit int32, exclude []uuid.UUID) ([]SyncCandidate, error) {
	if exclude == nil {
		exclude = []uuid.UUID{}
	}
	rows, err := s.repo.ListSyncCandidates(ctx, repo.ListSyncCandidatesParams{
		SyncIntervalSeconds:  int32(SyncInterval.Seconds()),
		ExcludeConnectionIds: exclude,
		LimitCount:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list okta application sync candidates: %w", err)
	}
	out := make([]SyncCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, SyncCandidate{
			ConnectionID:     row.ConnectionID,
			OrganizationID:   row.OrganizationID,
			OrganizationSlug: row.OrganizationSlug,
		})
	}
	return out, nil
}

// DeleteConnectionSnapshot removes every snapshot and run row of a connection;
// revoke calls it inside its own transaction.
func DeleteConnectionSnapshot(ctx context.Context, db repo.DBTX, organizationID string, connectionID uuid.UUID) error {
	q := repo.New(db)
	if _, err := q.DeleteApplicationsForConnection(ctx, repo.DeleteApplicationsForConnectionParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: connectionID,
	}); err != nil {
		return fmt.Errorf("delete applications snapshot: %w", err)
	}
	if _, err := q.DeleteReconcileRunsForConnection(ctx, repo.DeleteReconcileRunsForConnectionParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: connectionID,
	}); err != nil {
		return fmt.Errorf("delete reconcile runs: %w", err)
	}
	return nil
}

// ListSnapshot returns the applications with live assignment counts, live
// rows first, capped at limit.
func ListSnapshot(ctx context.Context, db repo.DBTX, organizationID string, connectionID uuid.UUID, includeRemoved bool, limit int32) ([]repo.ListApplicationsRow, error) {
	rows, err := repo.New(db).ListApplications(ctx, repo.ListApplicationsParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: connectionID,
		IncludeRemoved:               includeRemoved,
		LimitCount:                   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list applications snapshot: %w", err)
	}
	return rows, nil
}

// AppState is one live application as the last sync saw it.
type AppState struct {
	Active   bool
	Assigned bool
}

// GetAppState returns a live application's state, or nil when the snapshot has no such app.
func GetAppState(ctx context.Context, db repo.DBTX, organizationID string, connectionID uuid.UUID, oktaAppID string) (*AppState, error) {
	row, err := repo.New(db).GetApplicationState(ctx, repo.GetApplicationStateParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: connectionID,
		OktaAppID:                    oktaAppID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load application state: %w", err)
	}
	return &AppState{Active: row.Status == appStatusActive, Assigned: row.Assignments > 0}, nil
}

// LatestRun returns the newest reconcile run, or nil before the first.
func LatestRun(ctx context.Context, db repo.DBTX, organizationID string, connectionID uuid.UUID) (*repo.OktaApplicationReconcileRun, error) {
	run, err := repo.New(db).GetLatestReconcileRun(ctx, repo.GetLatestReconcileRunParams{
		OrganizationID:               organizationID,
		IdentityProviderConnectionID: connectionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load latest reconcile run: %w", err)
	}
	return &run, nil
}

// Run reconciles one connection's snapshot and records a run row. Business
// failures are recorded and return nil; retryable failures return an error
// unless final, in which case they are recorded and the watermark advances.
func (s *Syncer) Run(ctx context.Context, connectionID uuid.UUID, final bool) error {
	logger := s.logger.With(attr.SlogIdentityProviderConnectionID(connectionID.String()))

	target, err := s.repo.GetSyncTarget(ctx, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		logger.InfoContext(ctx, "okta application sync skipped: connection gone")
		return nil
	}
	if err != nil {
		return retryable(fmt.Errorf("load sync target: %w", err))
	}
	if target.Status != "verified" {
		logger.InfoContext(ctx, "okta application sync skipped: connection not verified")
		return nil
	}
	logger = logger.With(attr.SlogOrganizationID(target.OrganizationID))

	run, opened, err := s.openRun(ctx, target)
	if err != nil {
		return retryable(err)
	}
	if !opened {
		logger.InfoContext(ctx, "okta application sync skipped: connection revoked before the run opened")
		return nil
	}
	logger = logger.With(attr.SlogOktaReconcileRunID(run.ID.String()))

	client, err := s.client(target)
	if err != nil {
		return s.fail(ctx, logger, run, emptySnapshot(), reasonClientUnavailable, err)
	}

	snap, err := fetchSnapshot(ctx, client)
	if err != nil {
		if ctx.Err() != nil {
			return retryable(ctx.Err())
		}
		reason, retry := classify(err)
		if reason == reasonCredentialRejected {
			s.clients.Forget(target.RemoteSessionClientID)
		}
		if retry && !final {
			if _, ferr := s.finish(ctx, s.repo, run, snap, emptyDiff(), "failed", reason); ferr != nil && !errors.Is(ferr, pgx.ErrNoRows) {
				logger.ErrorContext(ctx, "record failed run", attr.SlogError(ferr))
			}
			s.metrics.record(ctx, reason, snap.Truncated())
			logger.WarnContext(ctx, "okta application sync failed, retrying", attr.SlogError(err), attr.SlogReason(reason))
			return retryable(err)
		}
		return s.fail(ctx, logger, run, snap, reason, err)
	}

	diff, err := s.apply(ctx, target, run, snap)
	switch {
	case errors.Is(err, errSuperseded):
		if _, ferr := s.finish(ctx, s.repo, run, snap, emptyDiff(), "failed", reasonSuperseded); ferr != nil && !errors.Is(ferr, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "record superseded run", attr.SlogError(ferr))
		}
		s.metrics.record(ctx, reasonSuperseded, snap.Truncated())
		logger.InfoContext(ctx, "okta application sync superseded by a later run")
		return nil
	case errors.Is(err, errConnectionGone):
		// Revoke deletes the run row, so a missing row is the expected case.
		if _, ferr := s.finish(ctx, s.repo, run, snap, emptyDiff(), "failed", reasonDiscarded); ferr != nil && !errors.Is(ferr, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "record discarded run", attr.SlogError(ferr))
		}
		s.metrics.record(ctx, reasonDiscarded, snap.Truncated())
		logger.InfoContext(ctx, "okta application sync discarded: connection no longer verified")
		return nil
	case err != nil:
		return retryable(err)
	}
	s.metrics.record(ctx, outcomeSucceeded, snap.Truncated())
	logger.InfoContext(ctx, "okta application sync completed",
		attr.SlogOktaApplicationsSeen(len(snap.Applications)),
		attr.SlogOktaApplicationsAdded(len(diff.AddedApplications)),
		attr.SlogOktaApplicationsRemoved(len(diff.RemovedApplications)),
		attr.SlogOktaAssignmentsAdded(len(diff.AddedAssignments)),
		attr.SlogOktaAssignmentsRemoved(len(diff.RemovedAssignments)),
		attr.SlogOktaApplicationsTruncated(snap.Truncated()),
	)
	return nil
}

// openRun closes interrupted runs, prunes history, and inserts the new run
// under the connection row lock revoke takes, so a revoke that lands after
// GetSyncTarget cannot be followed by a run row it never deleted.
func (s *Syncer) openRun(ctx context.Context, target repo.GetSyncTargetRow) (repo.OktaApplicationReconcileRun, bool, error) {
	var none repo.OktaApplicationReconcileRun
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return none, false, fmt.Errorf("begin open run: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := s.repo.WithTx(tx)

	_, err = q.LockSyncConnection(ctx, repo.LockSyncConnectionParams{
		ConnectionID:   target.ConnectionID,
		OrganizationID: target.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, false, nil
	}
	if err != nil {
		return none, false, fmt.Errorf("lock connection: %w", err)
	}
	interrupted, err := q.FailInterruptedReconcileRuns(ctx, repo.FailInterruptedReconcileRunsParams{
		OrganizationID:               target.OrganizationID,
		IdentityProviderConnectionID: target.ConnectionID,
	})
	if err != nil {
		return none, false, fmt.Errorf("close interrupted runs: %w", err)
	}
	if _, err := q.PruneReconcileRuns(ctx, repo.PruneReconcileRunsParams{
		OrganizationID:               target.OrganizationID,
		IdentityProviderConnectionID: target.ConnectionID,
		Before:                       pgtype.Timestamptz{Time: time.Now().Add(-runRetention).UTC(), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		return none, false, fmt.Errorf("prune reconcile runs: %w", err)
	}
	run, err := q.CreateReconcileRun(ctx, repo.CreateReconcileRunParams{
		OrganizationID:               target.OrganizationID,
		IdentityProviderConnectionID: target.ConnectionID,
	})
	if err != nil {
		return none, false, fmt.Errorf("create reconcile run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return none, false, fmt.Errorf("commit open run: %w", err)
	}
	for range interrupted {
		s.metrics.record(ctx, reasonInterrupted, false)
	}
	return run, true, nil
}

func (s *Syncer) client(target repo.GetSyncTargetRow) (okta.Client, error) {
	if s.clients == nil {
		return nil, errClientUnavailable
	}
	if !target.JsonWebKeySetID.Valid {
		return nil, fmt.Errorf("okta applications: managed client has no key set")
	}
	client, err := s.clients.Client(okta.Config{
		OrgURL:                target.OrgUrl,
		ClientID:              target.ClientID,
		AudienceFormat:        string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint),
		RemoteSessionClientID: target.RemoteSessionClientID,
		OrganizationID:        target.OrganizationID,
		JSONWebKeySetID:       target.JsonWebKeySetID.UUID,
		MaxPages:              maxAssignmentPages,
	})
	if err != nil {
		s.clients.Forget(target.RemoteSessionClientID)
		return nil, fmt.Errorf("build okta client: %w", err)
	}
	return client, nil
}

// fail records a terminal failure and advances the watermark so the interval
// governs the next attempt.
func (s *Syncer) fail(ctx context.Context, logger *slog.Logger, run repo.OktaApplicationReconcileRun, snap Snapshot, reason string, cause error) error {
	if _, err := s.finish(ctx, s.repo, run, snap, emptyDiff(), "failed", reason); err != nil {
		return retryable(err)
	}
	if _, err := s.repo.MarkApplicationsSynced(ctx, repo.MarkApplicationsSyncedParams{
		SyncedAt:                     run.StartedAt,
		IdentityProviderConnectionID: run.IdentityProviderConnectionID,
		OrganizationID:               run.OrganizationID,
	}); err != nil {
		return retryable(fmt.Errorf("advance sync watermark: %w", err))
	}
	s.metrics.record(ctx, reason, snap.Truncated())
	logger.WarnContext(ctx, "okta application sync failed", attr.SlogError(cause), attr.SlogReason(reason))
	return nil
}

func (s *Syncer) finish(ctx context.Context, q *repo.Queries, run repo.OktaApplicationReconcileRun, snap Snapshot, diff Diff, status, reason string) (repo.OktaApplicationReconcileRun, error) {
	skipped := snap.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	finished, err := q.FinishReconcileRun(ctx, repo.FinishReconcileRunParams{
		Status:              status,
		ApplicationsSeen:    clampInt32(len(snap.Applications)),
		ApplicationsAdded:   clampInt32(len(diff.AddedApplications)),
		ApplicationsRemoved: clampInt32(len(diff.RemovedApplications)),
		AssignmentsAdded:    clampInt32(len(diff.AddedAssignments)),
		AssignmentsRemoved:  clampInt32(len(diff.RemovedAssignments)),
		SkippedAppIds:       skipped,
		Truncated:           snap.Truncated(),
		Error:               pgtype.Text{String: reason, Valid: reason != ""},
		ID:                  run.ID,
		OrganizationID:      run.OrganizationID,
	})
	if err != nil {
		return repo.OktaApplicationReconcileRun{}, fmt.Errorf("finish reconcile run: %w", err)
	}
	return finished, nil
}

func emptySnapshot() Snapshot {
	return Snapshot{
		Applications:          nil,
		Assignments:           nil,
		Skipped:               []string{},
		ApplicationsTruncated: false,
		IncompleteUsers:       map[string]bool{},
		IncompleteGroups:      map[string]bool{},
	}
}

func emptyDiff() Diff {
	return Diff{AddedApplications: nil, RemovedApplications: nil, AddedAssignments: nil, RemovedAssignments: nil}
}

func clampInt32(n int) int32 {
	if n > int(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(n) //nolint:gosec // clamped above.
}

// classify maps a fetch error to a typed reason and whether to retry.
// Credential rejection is deterministic; everything else (rate limit, Okta
// 5xx, transport, signer) is retried.
func classify(err error) (string, bool) {
	if apiErr, ok := errors.AsType[*okta.APIError](err); ok {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests:
			return reasonRateLimited, true
		case http.StatusUnauthorized, http.StatusForbidden:
			return reasonCredentialRejected, false
		}
		if apiErr.StatusCode == http.StatusBadRequest && strings.HasPrefix(apiErr.ErrorCode, "invalid_client") {
			return reasonCredentialRejected, false
		}
	}
	return reasonOktaUnreachable, true
}

// fetchSnapshot lists applications, then assignments per application, and
// dedupes every key so one bulk upsert never sees a row twice. A listing that
// hits its page cap keeps the pages it fetched and marks the snapshot
// truncated.
func fetchSnapshot(ctx context.Context, client okta.Client) (Snapshot, error) {
	snap := emptySnapshot()

	apps, err := listApps(ctx, client)
	if errors.Is(err, okta.ErrTooManyPages) {
		snap.ApplicationsTruncated = true
	} else if err != nil {
		return snap, err
	}
	if len(apps) > maxApplicationsPerRun {
		apps = apps[:maxApplicationsPerRun]
		snap.ApplicationsTruncated = true
	}

	seenApps := make(map[string]bool, len(apps))
	seenAssignments := map[AssignmentKey]bool{}
	for _, app := range apps {
		if seenApps[app.ID] {
			continue
		}
		seenApps[app.ID] = true
		if IsInternalApplication(app.Name, app.SignOnMode) {
			snap.Skipped = append(snap.Skipped, app.ID)
			continue
		}
		features := slices.Clone(app.Features)
		for i := range features {
			features[i] = strings.ReplaceAll(features[i], ",", "")
		}
		slices.Sort(features)
		snap.Applications = append(snap.Applications, Application{
			ID:          app.ID,
			Label:       app.Label,
			Name:        app.Name,
			SignOnMode:  app.SignOnMode,
			Status:      app.Status,
			Features:    features,
			Created:     app.Created,
			LastUpdated: app.LastUpdated,
		})

		if len(snap.Assignments) >= maxAssignmentsPerRun {
			snap.IncompleteUsers[app.ID] = true
			snap.IncompleteGroups[app.ID] = true
			continue
		}

		users, err := listAppUsers(ctx, client, app.ID)
		if errors.Is(err, okta.ErrTooManyPages) {
			snap.IncompleteUsers[app.ID] = true
		} else if err != nil {
			return snap, err
		}
		for _, u := range users {
			if len(snap.Assignments) >= maxAssignmentsPerRun {
				snap.IncompleteUsers[app.ID] = true
				break
			}
			a := Assignment{AppID: app.ID, Kind: PrincipalKindUser, PrincipalID: u.ID, Scope: u.Scope}
			if !seenAssignments[a.key()] {
				seenAssignments[a.key()] = true
				snap.Assignments = append(snap.Assignments, a)
			}
		}

		groups, err := listAppGroups(ctx, client, app.ID)
		if errors.Is(err, okta.ErrTooManyPages) {
			snap.IncompleteGroups[app.ID] = true
		} else if err != nil {
			return snap, err
		}
		for _, g := range groups {
			if len(snap.Assignments) >= maxAssignmentsPerRun {
				snap.IncompleteGroups[app.ID] = true
				break
			}
			a := Assignment{AppID: app.ID, Kind: PrincipalKindGroup, PrincipalID: g.ID, Scope: ""}
			if !seenAssignments[a.key()] {
				seenAssignments[a.key()] = true
				snap.Assignments = append(snap.Assignments, a)
			}
		}
	}
	return snap, nil
}

func listApps(ctx context.Context, client okta.Client) ([]okta.App, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	apps, err := client.ListApps(ctx, okta.ListAppsRequest{Query: "", Status: "", Limit: listPageSize, MaxPages: maxApplicationPages})
	if err != nil {
		return apps, fmt.Errorf("list apps: %w", err)
	}
	return apps, nil
}

func listAppUsers(ctx context.Context, client okta.Client, appID string) ([]okta.AppUser, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	users, err := client.ListAppUsers(ctx, okta.ListAppUsersRequest{AppID: appID, Limit: listPageSize, MaxPages: maxAssignmentPages})
	if err != nil {
		return users, fmt.Errorf("list app users: %w", err)
	}
	return users, nil
}

func listAppGroups(ctx context.Context, client okta.Client, appID string) ([]okta.AppGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	groups, err := client.ListAppGroups(ctx, okta.ListAppGroupsRequest{AppID: appID, Limit: listPageSize, MaxPages: maxAssignmentPages})
	if err != nil {
		return groups, fmt.Errorf("list app groups: %w", err)
	}
	return groups, nil
}

// apply writes the snapshot in one transaction under the connection row lock:
// upsert what was seen, remove what was not, record the run, and advance the
// watermark to the run's start. A run that started before an already-applied
// one, or whose row was closed by finalization, is superseded; a connection
// revoked meanwhile discards the run.
func (s *Syncer) apply(ctx context.Context, target repo.GetSyncTargetRow, run repo.OktaApplicationReconcileRun, snap Snapshot) (Diff, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Diff{}, fmt.Errorf("begin apply: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := s.repo.WithTx(tx)

	_, err = q.LockSyncConnection(ctx, repo.LockSyncConnectionParams{
		ConnectionID:   target.ConnectionID,
		OrganizationID: target.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Diff{}, errConnectionGone
	}
	if err != nil {
		return Diff{}, fmt.Errorf("lock connection: %w", err)
	}
	syncedAt, err := q.GetApplicationsSyncedAt(ctx, repo.GetApplicationsSyncedAtParams{
		IdentityProviderConnectionID: target.ConnectionID,
		OrganizationID:               target.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Diff{}, errConnectionGone
	}
	if err != nil {
		return Diff{}, fmt.Errorf("read applications watermark: %w", err)
	}
	if syncedAt.Valid && syncedAt.Time.After(run.StartedAt.Time) {
		return Diff{}, errSuperseded
	}

	scope := repo.ListLiveApplicationIDsParams{OrganizationID: target.OrganizationID, IdentityProviderConnectionID: target.ConnectionID}
	liveApps, err := q.ListLiveApplicationIDs(ctx, scope)
	if err != nil {
		return Diff{}, fmt.Errorf("list live applications: %w", err)
	}
	liveRows, err := q.ListLiveAssignmentKeys(ctx, repo.ListLiveAssignmentKeysParams(scope))
	if err != nil {
		return Diff{}, fmt.Errorf("list live assignments: %w", err)
	}
	liveAssignments := make([]AssignmentKey, 0, len(liveRows))
	for _, r := range liveRows {
		liveAssignments = append(liveAssignments, AssignmentKey{AppID: r.OktaAppID, Kind: r.PrincipalKind, PrincipalID: r.OktaPrincipalID})
	}

	diff := Reconcile(liveApps, liveAssignments, snap)
	seenAt := run.StartedAt

	for chunk := range chunks(snap.Applications, upsertChunk) {
		params := repo.UpsertApplicationsParams{
			OrganizationID:               target.OrganizationID,
			IdentityProviderConnectionID: target.ConnectionID,
			SeenAt:                       seenAt,
			OktaAppIds:                   make([]string, 0, len(chunk)),
			Labels:                       make([]string, 0, len(chunk)),
			Names:                        make([]string, 0, len(chunk)),
			SignOnModes:                  make([]string, 0, len(chunk)),
			Statuses:                     make([]string, 0, len(chunk)),
			FeaturesCsv:                  make([]string, 0, len(chunk)),
			OktaCreatedAts:               make([]pgtype.Timestamptz, 0, len(chunk)),
			OktaLastUpdatedAts:           make([]pgtype.Timestamptz, 0, len(chunk)),
		}
		for _, app := range chunk {
			params.OktaAppIds = append(params.OktaAppIds, app.ID)
			params.Labels = append(params.Labels, app.Label)
			params.Names = append(params.Names, app.Name)
			params.SignOnModes = append(params.SignOnModes, app.SignOnMode)
			params.Statuses = append(params.Statuses, app.Status)
			params.FeaturesCsv = append(params.FeaturesCsv, strings.Join(app.Features, ","))
			params.OktaCreatedAts = append(params.OktaCreatedAts, optionalTime(app.Created))
			params.OktaLastUpdatedAts = append(params.OktaLastUpdatedAts, optionalTime(app.LastUpdated))
		}
		if err := q.UpsertApplications(ctx, params); err != nil {
			return Diff{}, fmt.Errorf("upsert applications: %w", err)
		}
	}

	if len(diff.RemovedApplications) > 0 {
		if err := q.RemoveApplications(ctx, repo.RemoveApplicationsParams{
			RemovedAt:                    seenAt,
			OrganizationID:               target.OrganizationID,
			IdentityProviderConnectionID: target.ConnectionID,
			OktaAppIds:                   diff.RemovedApplications,
		}); err != nil {
			return Diff{}, fmt.Errorf("remove applications: %w", err)
		}
	}

	for chunk := range chunks(snap.Assignments, upsertChunk) {
		params := repo.UpsertAssignmentsParams{
			OrganizationID:               target.OrganizationID,
			IdentityProviderConnectionID: target.ConnectionID,
			SeenAt:                       seenAt,
			OktaAppIds:                   make([]string, 0, len(chunk)),
			PrincipalKinds:               make([]string, 0, len(chunk)),
			OktaPrincipalIds:             make([]string, 0, len(chunk)),
			AssignmentScopes:             make([]string, 0, len(chunk)),
		}
		for _, a := range chunk {
			params.OktaAppIds = append(params.OktaAppIds, a.AppID)
			params.PrincipalKinds = append(params.PrincipalKinds, a.Kind)
			params.OktaPrincipalIds = append(params.OktaPrincipalIds, a.PrincipalID)
			params.AssignmentScopes = append(params.AssignmentScopes, a.Scope)
		}
		if err := q.UpsertAssignments(ctx, params); err != nil {
			return Diff{}, fmt.Errorf("upsert assignments: %w", err)
		}
	}

	for chunk := range chunks(diff.RemovedAssignments, upsertChunk) {
		params := repo.RemoveAssignmentsParams{
			RemovedAt:                    seenAt,
			OktaAppIds:                   make([]string, 0, len(chunk)),
			PrincipalKinds:               make([]string, 0, len(chunk)),
			OktaPrincipalIds:             make([]string, 0, len(chunk)),
			OrganizationID:               target.OrganizationID,
			IdentityProviderConnectionID: target.ConnectionID,
		}
		for _, k := range chunk {
			params.OktaAppIds = append(params.OktaAppIds, k.AppID)
			params.PrincipalKinds = append(params.PrincipalKinds, k.Kind)
			params.OktaPrincipalIds = append(params.OktaPrincipalIds, k.PrincipalID)
		}
		if err := q.RemoveAssignments(ctx, params); err != nil {
			return Diff{}, fmt.Errorf("remove assignments: %w", err)
		}
	}

	if _, err := s.finish(ctx, q, run, snap, diff, "succeeded", ""); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Diff{}, errSuperseded
		}
		return Diff{}, err
	}
	advanced, err := q.MarkApplicationsSynced(ctx, repo.MarkApplicationsSyncedParams{
		SyncedAt:                     seenAt,
		IdentityProviderConnectionID: target.ConnectionID,
		OrganizationID:               target.OrganizationID,
	})
	if err != nil {
		return Diff{}, fmt.Errorf("advance sync watermark: %w", err)
	}
	if advanced == 0 {
		return Diff{}, errConnectionGone
	}
	if err := tx.Commit(ctx); err != nil {
		return Diff{}, fmt.Errorf("commit apply: %w", err)
	}
	return diff, nil
}

func optionalTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true, InfinityModifier: pgtype.Finite}
}

// chunks yields consecutive slices of at most size elements.
func chunks[T any](items []T, size int) func(yield func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(items); start += size {
			end := min(start+size, len(items))
			if !yield(items[start:end]) {
				return
			}
		}
	}
}

// FinalizeFailure fences attempts still executing after Temporal has exhausted
// retries. Both times are fixed by the workflow, not by this retryable
// activity: cutoff (the observed failure) bounds which running rows close,
// startedAt (recorded before the first attempt) is as far as the watermark
// moves, so a sync requested during the attempt stays due.
func (s *Syncer) FinalizeFailure(ctx context.Context, connectionID uuid.UUID, startedAt, cutoff time.Time) error {
	// Match PostgreSQL timestamp precision so a retried payload with nanoseconds
	// compares equal to the watermark written by its first attempt.
	startedAt = startedAt.UTC().Truncate(time.Microsecond)
	cutoff = cutoff.UTC().Truncate(time.Microsecond)
	if cutoff.Before(startedAt) {
		cutoff = startedAt
	}
	target, err := s.repo.GetSyncTarget(ctx, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load finalization target: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin failure finalization: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := s.repo.WithTx(tx)
	_, err = q.LockSyncConnection(ctx, repo.LockSyncConnectionParams{ConnectionID: connectionID, OrganizationID: target.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock finalization connection: %w", err)
	}
	syncedAt, err := q.GetApplicationsSyncedAt(ctx, repo.GetApplicationsSyncedAtParams{
		IdentityProviderConnectionID: connectionID, OrganizationID: target.OrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read finalization watermark: %w", err)
	}
	if syncedAt.Valid && !syncedAt.Time.Before(startedAt) {
		return nil
	}
	if _, err := q.FinalizeInterruptedReconcileRuns(ctx, repo.FinalizeInterruptedReconcileRunsParams{
		OrganizationID: target.OrganizationID, IdentityProviderConnectionID: connectionID, Cutoff: optionalTime(cutoff),
	}); err != nil {
		return fmt.Errorf("close terminal attempts: %w", err)
	}
	if _, err := q.MarkApplicationsSynced(ctx, repo.MarkApplicationsSyncedParams{
		OrganizationID: target.OrganizationID, IdentityProviderConnectionID: connectionID, SyncedAt: optionalTime(startedAt),
	}); err != nil {
		return fmt.Errorf("advance terminal watermark: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failure finalization: %w", err)
	}
	return nil
}
