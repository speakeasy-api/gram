package oktaapplications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	// maxApplicationsPerRun caps the snapshot; larger tenants get a truncated
	// run rather than an unbounded one.
	maxApplicationsPerRun = 2000

	// listPageSize is the Okta page size for every listing.
	listPageSize = 200

	// maxPages caps how many pages one listing follows.
	maxPages = 200

	// oktaCallTimeout bounds one listing (all of its pages).
	oktaCallTimeout = 5 * time.Minute

	// upsertChunk is how many rows one bulk statement carries.
	upsertChunk = 500
)

// ErrClientUnavailable is recorded when no Okta client can be built.
var ErrClientUnavailable = errors.New("okta applications: client factory not configured")

// retryableError marks a failure Temporal should retry: our own infrastructure
// or an Okta rate limit. Everything else is recorded on the run and returns nil.
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

// Run reconciles one connection's snapshot and records a run row. Business
// failures are recorded and return nil; retryable failures return an error.
func (s *Syncer) Run(ctx context.Context, connectionID uuid.UUID) error {
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

	if err := s.repo.FailInterruptedReconcileRuns(ctx, repo.FailInterruptedReconcileRunsParams{
		OrganizationID:               target.OrganizationID,
		IdentityProviderConnectionID: target.ConnectionID,
	}); err != nil {
		return retryable(fmt.Errorf("close interrupted runs: %w", err))
	}
	run, err := s.repo.CreateReconcileRun(ctx, repo.CreateReconcileRunParams{
		OrganizationID:               target.OrganizationID,
		IdentityProviderConnectionID: target.ConnectionID,
	})
	if err != nil {
		return retryable(fmt.Errorf("create reconcile run: %w", err))
	}

	client, err := s.client(target)
	if err != nil {
		logger.ErrorContext(ctx, "okta application sync cannot build client", attr.SlogError(err))
		return s.fail(ctx, logger, run, emptySnapshot(), reasonClientUnavailable, true)
	}

	snap, err := fetchSnapshot(ctx, client)
	if err != nil {
		if ctx.Err() != nil {
			return retryable(ctx.Err())
		}
		reason, retry := classify(err)
		logger.WarnContext(ctx, "okta application sync fetch failed", attr.SlogError(err), attr.SlogReason(reason))
		if reason == reasonCredentialRejected {
			s.clients.Forget(target.RemoteSessionClientID)
		}
		if retry {
			if _, ferr := s.finish(ctx, run, snap, emptyDiff(), "failed", reason); ferr != nil {
				logger.ErrorContext(ctx, "record failed run", attr.SlogError(ferr))
			}
			s.metrics.record(ctx, reason, snap.Truncated())
			return retryable(err)
		}
		return s.fail(ctx, logger, run, snap, reason, true)
	}

	diff, err := s.apply(ctx, target, run, snap)
	if err != nil {
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

func (s *Syncer) client(target repo.GetSyncTargetRow) (okta.Client, error) {
	if s.clients == nil {
		return nil, ErrClientUnavailable
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
		MaxPages:              maxPages,
	})
	if err != nil {
		s.clients.Forget(target.RemoteSessionClientID)
		return nil, fmt.Errorf("build okta client: %w", err)
	}
	return client, nil
}

// fail records a business failure and advances the watermark so the interval
// governs the next attempt.
func (s *Syncer) fail(ctx context.Context, logger *slog.Logger, run repo.OktaApplicationReconcileRun, snap Snapshot, reason string, advance bool) error {
	if _, err := s.finish(ctx, run, snap, emptyDiff(), "failed", reason); err != nil {
		return retryable(fmt.Errorf("record failed run: %w", err))
	}
	if advance {
		if err := s.repo.MarkApplicationsSynced(ctx, repo.MarkApplicationsSyncedParams{
			IdentityProviderConnectionID: run.IdentityProviderConnectionID,
			OrganizationID:               run.OrganizationID,
		}); err != nil {
			return retryable(fmt.Errorf("advance sync watermark: %w", err))
		}
	}
	s.metrics.record(ctx, reason, snap.Truncated())
	logger.WarnContext(ctx, "okta application sync failed", attr.SlogReason(reason))
	return nil
}

func (s *Syncer) finish(ctx context.Context, run repo.OktaApplicationReconcileRun, snap Snapshot, diff Diff, status, reason string) (repo.OktaApplicationReconcileRun, error) {
	skipped := snap.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	finished, err := s.repo.FinishReconcileRun(ctx, repo.FinishReconcileRunParams{
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
		IncompleteAssignments: map[string]bool{},
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
func classify(err error) (string, bool) {
	if errors.Is(err, okta.ErrTooManyPages) {
		return reasonTooManyApplications, false
	}
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
	return reasonOktaUnreachable, false
}

// fetchSnapshot lists applications, then assignments per application.
func fetchSnapshot(ctx context.Context, client okta.Client) (Snapshot, error) {
	snap := emptySnapshot()

	apps, err := listApps(ctx, client)
	if err != nil {
		return snap, err
	}
	if len(apps) > maxApplicationsPerRun {
		apps = apps[:maxApplicationsPerRun]
		snap.ApplicationsTruncated = true
	}

	for _, app := range apps {
		if IsInternalApplication(app.Name, app.SignOnMode) {
			snap.Skipped = append(snap.Skipped, app.ID)
			continue
		}
		snap.Applications = append(snap.Applications, Application{
			ID:          app.ID,
			Label:       app.Label,
			Name:        app.Name,
			SignOnMode:  app.SignOnMode,
			Status:      app.Status,
			Features:    app.Features,
			Created:     app.Created,
			LastUpdated: app.LastUpdated,
		})

		users, err := listAppUsers(ctx, client, app.ID)
		if errors.Is(err, okta.ErrTooManyPages) {
			snap.IncompleteAssignments[app.ID] = true
			users = nil
		} else if err != nil {
			return snap, err
		}
		for _, u := range users {
			snap.Assignments = append(snap.Assignments, Assignment{AppID: app.ID, Kind: PrincipalKindUser, PrincipalID: u.ID, Scope: u.Scope})
		}

		groups, err := listAppGroups(ctx, client, app.ID)
		if errors.Is(err, okta.ErrTooManyPages) {
			snap.IncompleteAssignments[app.ID] = true
			groups = nil
		} else if err != nil {
			return snap, err
		}
		for _, g := range groups {
			snap.Assignments = append(snap.Assignments, Assignment{AppID: app.ID, Kind: PrincipalKindGroup, PrincipalID: g.ID, Scope: ""})
		}
	}
	return snap, nil
}

func listApps(ctx context.Context, client okta.Client) ([]okta.App, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	apps, err := client.ListApps(ctx, okta.ListAppsRequest{Query: "", Status: "", Limit: listPageSize})
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	return apps, nil
}

func listAppUsers(ctx context.Context, client okta.Client, appID string) ([]okta.AppUser, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	users, err := client.ListAppUsers(ctx, okta.ListAppUsersRequest{AppID: appID, Limit: listPageSize})
	if err != nil {
		return nil, fmt.Errorf("list app users: %w", err)
	}
	return users, nil
}

func listAppGroups(ctx context.Context, client okta.Client, appID string) ([]okta.AppGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, oktaCallTimeout)
	defer cancel()
	groups, err := client.ListAppGroups(ctx, okta.ListAppGroupsRequest{AppID: appID, Limit: listPageSize})
	if err != nil {
		return nil, fmt.Errorf("list app groups: %w", err)
	}
	return groups, nil
}

// apply writes the snapshot in one transaction: upsert what was seen, remove
// what was not, record the run, and advance the watermark.
func (s *Syncer) apply(ctx context.Context, target repo.GetSyncTargetRow, run repo.OktaApplicationReconcileRun, snap Snapshot) (Diff, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Diff{}, fmt.Errorf("begin apply: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := s.repo.WithTx(tx)

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
	seenAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true, InfinityModifier: pgtype.Finite}

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

	if _, err := s.repo.WithTx(tx).FinishReconcileRun(ctx, repo.FinishReconcileRunParams{
		Status:              "succeeded",
		ApplicationsSeen:    clampInt32(len(snap.Applications)),
		ApplicationsAdded:   clampInt32(len(diff.AddedApplications)),
		ApplicationsRemoved: clampInt32(len(diff.RemovedApplications)),
		AssignmentsAdded:    clampInt32(len(diff.AddedAssignments)),
		AssignmentsRemoved:  clampInt32(len(diff.RemovedAssignments)),
		SkippedAppIds:       snap.Skipped,
		Truncated:           snap.Truncated(),
		Error:               pgtype.Text{String: "", Valid: false},
		ID:                  run.ID,
		OrganizationID:      run.OrganizationID,
	}); err != nil {
		return Diff{}, fmt.Errorf("finish reconcile run: %w", err)
	}
	if err := q.MarkApplicationsSynced(ctx, repo.MarkApplicationsSyncedParams{
		IdentityProviderConnectionID: target.ConnectionID,
		OrganizationID:               target.OrganizationID,
	}); err != nil {
		return Diff{}, fmt.Errorf("advance sync watermark: %w", err)
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
