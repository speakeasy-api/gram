package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// IssuerMetadataRefreshActor is the system principal the on-use refresh audits as.
	IssuerMetadataRefreshActor = "issuer-metadata-refresh"

	// IssuerMetadataRefreshActorDisplayName is the name the audit feed shows for that principal.
	IssuerMetadataRefreshActorDisplayName = "Issuer metadata refresh"

	// issuerMetadataStaleAfter is how long a visit, successful or not, keeps an issuer from being refreshed on use.
	issuerMetadataStaleAfter = 24 * time.Hour

	// issuerMetadataRetryAfter is how soon a transiently unreadable candidate is retried on use.
	issuerMetadataRetryAfter = time.Hour

	// issuerMetadataRefreshSlots bounds the refreshes one replica runs at once; a use that finds no slot is skipped and the next use tries again.
	issuerMetadataRefreshSlots = 4

	// issuerMetadataRefreshBudget caps one detached refresh: discovery's own ten-second budget plus the writes.
	issuerMetadataRefreshBudget = 30 * time.Second

	// issuerMetadataRefreshLockBudget caps the lock round trip so a Redis stall never pins every slot for the whole budget.
	issuerMetadataRefreshLockBudget = 2 * time.Second

	// issuerMetadataRefreshLockTTL outlives one refresh budget; the row's own stamps pace retries, so a crash costs at most this long.
	issuerMetadataRefreshLockTTL = 2 * issuerMetadataRefreshBudget

	issuerMetadataRefreshLockPrefix = "remote_session_issuer_metadata_refresh:"
)

// IssuerMetadataRefreshCandidate is one issuer to refresh, keyed by the identity every write re-asserts.
type IssuerMetadataRefreshCandidate struct {
	ID             uuid.UUID
	IssuerURL      string
	ProjectID      uuid.NullUUID
	OrganizationID pgtype.Text
}

// IssuerMetadataUse is what a flow-time row says about its issuer's metadata; NoteUse decides from it alone.
type IssuerMetadataUse struct {
	ID                   uuid.UUID
	IssuerURL            string
	ProjectID            uuid.NullUUID
	OrganizationID       pgtype.Text
	MetadataFetchedAt    pgtype.Timestamptz
	MetadataLastErrorAt  pgtype.Timestamptz
	MetadataLastErrorUrl pgtype.Text
	// NeedsReprojection: a stored document exists and a capability column is still NULL.
	NeedsReprojection bool
}

func (u IssuerMetadataUse) candidate() IssuerMetadataRefreshCandidate {
	return IssuerMetadataRefreshCandidate{
		ID:             u.ID,
		IssuerURL:      u.IssuerURL,
		ProjectID:      u.ProjectID,
		OrganizationID: u.OrganizationID,
	}
}

func issuerUseFromClientRow(row repo.GetRemoteSessionClientWithIssuerByIDRow) IssuerMetadataUse {
	return IssuerMetadataUse{
		ID:                   row.RemoteSessionIssuerID,
		IssuerURL:            row.IssuerUrl,
		ProjectID:            row.IssuerProjectID,
		OrganizationID:       row.IssuerOrganizationID,
		MetadataFetchedAt:    row.MetadataFetchedAt,
		MetadataLastErrorAt:  row.MetadataLastErrorAt,
		MetadataLastErrorUrl: row.MetadataLastErrorUrl,
		NeedsReprojection:    row.MetadataNeedsReprojection,
	}
}

func issuerUseFromClientListRow(row repo.ListRemoteSessionClientsForUserSessionIssuerRow) IssuerMetadataUse {
	return IssuerMetadataUse{
		ID:                   row.RemoteSessionIssuerID,
		IssuerURL:            row.IssuerUrl,
		ProjectID:            row.IssuerProjectID,
		OrganizationID:       row.IssuerOrganizationID,
		MetadataFetchedAt:    row.MetadataFetchedAt,
		MetadataLastErrorAt:  row.MetadataLastErrorAt,
		MetadataLastErrorUrl: row.MetadataLastErrorUrl,
		NeedsReprojection:    row.MetadataNeedsReprojection,
	}
}

// issuerMetadataPlan is what one use of an issuer should do to its metadata.
type issuerMetadataPlan struct {
	reproject bool
	fetch     bool
}

func (p issuerMetadataPlan) empty() bool { return !p.reproject && !p.fetch }

// planIssuerMetadataRefresh: fetch when never visited, when the last visit (fetched_at or last_error_at) is stale, or when a transient failure is past its retry window; re-project when a stored document has a NULL capability column and no definitive failure stands.
func planIssuerMetadataRefresh(use IssuerMetadataUse, now time.Time) issuerMetadataPlan {
	visited, visitedAt := lastIssuerMetadataVisit(use)
	transient := use.MetadataLastErrorUrl.Valid && use.MetadataLastErrorUrl.String != ""
	definitiveFailure := use.MetadataLastErrorAt.Valid && !transient

	return issuerMetadataPlan{
		reproject: use.NeedsReprojection && !definitiveFailure,
		fetch: !visited ||
			now.Sub(visitedAt) >= issuerMetadataStaleAfter ||
			(transient && use.MetadataLastErrorAt.Valid && now.Sub(use.MetadataLastErrorAt.Time) >= issuerMetadataRetryAfter),
	}
}

func lastIssuerMetadataVisit(use IssuerMetadataUse) (bool, time.Time) {
	var visitedAt time.Time
	visited := false
	for _, ts := range []pgtype.Timestamptz{use.MetadataFetchedAt, use.MetadataLastErrorAt} {
		if ts.Valid && (!visited || ts.Time.After(visitedAt)) {
			visited, visitedAt = true, ts.Time
		}
	}
	return visited, visitedAt
}

// IssuerMetadataRefresher keeps RFC 8414 issuer metadata fresh by refreshing an issuer when a session flow uses it.
type IssuerMetadataRefresher struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	policy      *guardian.Policy
	auditLogger *audit.Logger
	locks       cache.Cache
	metrics     *remotesessionmetrics.IssuerMetadataRefresh

	slots chan struct{}
	wg    sync.WaitGroup
}

// NewIssuerMetadataRefresher wires the on-use refresh; locks dedupes refreshes of one issuer across replicas.
func NewIssuerMetadataRefresher(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, policy *guardian.Policy, auditLogger *audit.Logger, locks cache.Cache) *IssuerMetadataRefresher {
	return &IssuerMetadataRefresher{
		logger:      logger.With(attr.SlogComponent("remotesessions_issuer_metadata_refresh")),
		db:          db,
		policy:      policy,
		auditLogger: auditLogger,
		locks:       locks,
		metrics:     remotesessionmetrics.NewIssuerMetadataRefresh(logger, meterProvider),
		slots:       make(chan struct{}, issuerMetadataRefreshSlots),
		wg:          sync.WaitGroup{},
	}
}

// NoteUse records that a session flow is using the issuer and, when its metadata is due, refreshes it off the request path. The caller keeps the stored row; the next use sees the result. Safe on a nil receiver.
func (r *IssuerMetadataRefresher) NoteUse(ctx context.Context, use IssuerMetadataUse) {
	if r == nil {
		return
	}
	plan := planIssuerMetadataRefresh(use, time.Now())
	if plan.empty() {
		return
	}
	candidate := use.candidate()
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(use.ID.String()), attr.SlogOAuthIssuer(use.IssuerURL))

	// The slot is taken on the request path so a busy replica answers at once; the lock and the work run detached, so a client that disconnects mid-render still gets the refresh.
	select {
	case r.slots <- struct{}{}:
	default:
		r.metrics.Record(ctx, candidate.IssuerURL, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedBusy)
		return
	}

	// Only the trace carries over: the request's authenticated actor and OAuth client must not reach the audit entry, which is the system's.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	r.wg.Go(func() {
		defer func() { <-r.slots }()
		ctx, cancel := context.WithTimeout(detached, issuerMetadataRefreshBudget)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(ctx, "issuer metadata refresh panicked", attr.SlogError(fmt.Errorf("%v", rec)))
				r.metrics.Record(ctx, candidate.IssuerURL, remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError)
			}
		}()

		lockKey := issuerMetadataRefreshLockPrefix + use.ID.String()
		if !r.acquire(ctx, logger, lockKey, candidate.IssuerURL) {
			return
		}
		defer r.release(ctx, logger, lockKey)
		r.run(ctx, logger, candidate, plan)
	})
}

// acquire takes the per-issuer lock so no two replicas refresh one issuer at once.
func (r *IssuerMetadataRefresher) acquire(ctx context.Context, logger *slog.Logger, lockKey, issuerURL string) bool {
	lockCtx, cancel := context.WithTimeout(ctx, issuerMetadataRefreshLockBudget)
	defer cancel()
	won, err := r.locks.Add(lockCtx, lockKey, issuerMetadataRefreshLockTTL)
	if err != nil {
		logger.WarnContext(ctx, "issuer metadata refresh lock unavailable; leaving the stored metadata in place", attr.SlogError(err))
		r.metrics.Record(ctx, issuerURL, remotesessionmetrics.IssuerMetadataRefreshOutcomeLockUnavailable)
		return false
	}
	if !won {
		r.metrics.Record(ctx, issuerURL, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedLocked)
		return false
	}
	return true
}

func (r *IssuerMetadataRefresher) release(ctx context.Context, logger *slog.Logger, lockKey string) {
	lockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerMetadataRefreshLockBudget)
	defer cancel()
	if err := r.locks.Delete(lockCtx, lockKey); err != nil {
		logger.WarnContext(ctx, "release issuer metadata refresh lock", attr.SlogError(err))
	}
}

// Wait blocks until every refresh NoteUse started has finished.
func (r *IssuerMetadataRefresher) Wait() {
	if r == nil {
		return
	}
	r.wg.Wait()
}

func (r *IssuerMetadataRefresher) run(ctx context.Context, logger *slog.Logger, candidate IssuerMetadataRefreshCandidate, plan issuerMetadataPlan) {
	if plan.reproject {
		if _, err := r.Reproject(ctx, candidate); err != nil {
			logger.ErrorContext(ctx, "reproject issuer metadata on use", attr.SlogError(err))
		}
	}
	if plan.fetch {
		if _, err := r.Refresh(ctx, candidate); err != nil {
			logger.ErrorContext(ctx, "refresh issuer metadata on use", attr.SlogError(err))
		}
	}
}

// Reproject rewrites an issuer's capability columns from its stored document, leaving the document and the tracking columns alone.
func (r *IssuerMetadataRefresher) Reproject(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer))

	doc, err := decodeStoredIssuerDocument(existing)
	if err != nil {
		logger.WarnContext(ctx, "stored issuer metadata document cannot be re-projected", attr.SlogError(err))
		msg := "stored issuer metadata document could not be decoded"
		if ude, ok := errors.AsType[*untrustedDocumentError](err); ok {
			msg = ude.reason
		}
		outcome, err := r.recordFailure(ctx, existing, msg, "", remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojectInvalid)
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}

	outcome, err = r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.ReprojectRemoteSessionIssuerMetadataCapabilities(ctx, repo.ReprojectRemoteSessionIssuerMetadataCapabilitiesParams{
			CodeChallengeMethodsSupported:              orEmptySlice(doc.CodeChallengeMethodsSupported),
			UserinfoEndpoint:                           doc.UserinfoEndpoint,
			IntrospectionEndpoint:                      doc.IntrospectionEndpoint,
			IntrospectionEndpointAuthMethodsSupported:  orEmptySlice(doc.IntrospectionEndpointAuthMethodsSupported),
			IDTokenSigningAlgValuesSupported:           orEmptySlice(doc.IDTokenSigningAlgValuesSupported),
			ClaimsSupported:                            orEmptySlice(doc.ClaimsSupported),
			BackchannelLogoutSupported:                 doc.BackchannelLogoutSupported,
			AuthorizationResponseIssParameterSupported: doc.AuthorizationResponseIssParameterSupported,
			ID:             existing.ID,
			Issuer:         existing.Issuer,
			ProjectID:      existing.ProjectID,
			OrganizationID: existing.OrganizationID,
		})
	}, remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected)
	return r.record(ctx, candidate.IssuerURL, outcome), err
}

// Refresh fetches an issuer's upstream metadata and applies it; discovery failures are outcomes, not errors.
func (r *IssuerMetadataRefresher) Refresh(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer))

	params, _, err := refreshIssuerMetadata(ctx, r.policy, existing)
	if err != nil {
		msg, _ := discoveryFailureMessage(err)
		retryURL := discoveryRetryURL(err)
		failure := remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure
		if retryURL != "" {
			failure = remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure
		}
		logger.WarnContext(ctx, "issuer metadata refresh on use failed", attr.SlogOutcome(string(failure)), attr.SlogError(err))
		outcome, err := r.recordFailure(ctx, existing, msg, retryURL, failure)
		return r.record(ctx, candidate.IssuerURL, outcome), err
	}

	success := remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed
	if params.MetadataLastErrorUrl != "" {
		success = remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshedPartial
	}
	outcome, err = r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	}, success)
	return r.record(ctx, candidate.IssuerURL, outcome), err
}

// load reads the row under the listed identity; a miss is a conflict outcome.
func (r *IssuerMetadataRefresher) load(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (repo.RemoteSessionIssuer, remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	var zero repo.RemoteSessionIssuer
	existing, err := repo.New(r.db).GetRemoteSessionIssuerForMetadataRefresh(ctx, repo.GetRemoteSessionIssuerForMetadataRefreshParams{
		ID:             candidate.ID,
		Issuer:         candidate.IssuerURL,
		ProjectID:      candidate.ProjectID,
		OrganizationID: candidate.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return zero, remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("get remote session issuer: %w", err)
	}
	return existing, "", nil
}

func decodeStoredIssuerDocument(existing repo.RemoteSessionIssuer) (rfc8414Document, error) {
	requested, err := url.Parse(existing.Issuer)
	if err != nil {
		return rfc8414Document{}, fmt.Errorf("parse issuer url: %w", err)
	}
	if len(existing.Metadata) == 0 {
		return rfc8414Document{}, errors.New("no stored metadata document")
	}
	doc, err := decodeIssuerDocument(existing.Metadata, requested)
	if err != nil {
		return rfc8414Document{}, err
	}
	if err := vetRefreshedDocument(doc, existing); err != nil {
		return rfc8414Document{}, err
	}
	return doc, nil
}

func (r *IssuerMetadataRefresher) record(ctx context.Context, issuerURL string, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) remotesessionmetrics.IssuerMetadataRefreshOutcome {
	r.metrics.Record(ctx, issuerURL, outcome)
	return outcome
}

// recordFailure stamps the error trio on the row; a retry URL marks the failure transient.
func (r *IssuerMetadataRefresher) recordFailure(ctx context.Context, existing repo.RemoteSessionIssuer, msg, retryURL string, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	rows, err := repo.New(r.db).RecordRemoteSessionIssuerMetadataRefreshFailure(ctx, repo.RecordRemoteSessionIssuerMetadataRefreshFailureParams{
		MetadataLastError:         msg,
		MetadataLastErrorUrl:      retryURL,
		ObservedMetadataFetchedAt: existing.MetadataFetchedAt,
		ID:                        existing.ID,
		Issuer:                    existing.Issuer,
		ProjectID:                 existing.ProjectID,
		OrganizationID:            existing.OrganizationID,
	})
	if err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("record issuer metadata refresh failure: %w", err)
	}
	if rows == 0 {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
	}
	return outcome, nil
}

// apply runs the write and the system-actor audit entry in one transaction, snapshotting the row under lock.
func (r *IssuerMetadataRefresher) apply(ctx context.Context, logger *slog.Logger, existing repo.RemoteSessionIssuer, write func(*repo.Queries) (repo.RemoteSessionIssuer, error), success remotesessionmetrics.IssuerMetadataRefreshOutcome) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	dbtx, err := r.db.Begin(ctx)
	if err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	locked, err := txRepo.LockRemoteSessionIssuerForMetadataRefresh(ctx, repo.LockRemoteSessionIssuerForMetadataRefreshParams{
		ID:             existing.ID,
		Issuer:         existing.Issuer,
		ProjectID:      existing.ProjectID,
		OrganizationID: existing.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("lock remote session issuer: %w", err)
	}

	// Any write that landed between the read and this lock, an operator edit or refresh, wins; overwriting it would drop the newer values.
	if !sameTimestamp(locked.MetadataFetchedAt, existing.MetadataFetchedAt) || !sameTimestamp(locked.UpdatedAt, existing.UpdatedAt) {
		logger.InfoContext(ctx, "issuer changed during refresh; leaving the newer row in place", attr.SlogOutcome(string(remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict)))
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
	}

	updated, err := write(txRepo)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("write remote session issuer metadata: %w", err)
	}

	// Only a row with neither scope column is global; a legacy project row resolves its organization through the project.
	organizationID := updated.OrganizationID.String
	if !updated.OrganizationID.Valid && updated.ProjectID.Valid {
		organizationID, err = txRepo.GetProjectOrganizationID(ctx, updated.ProjectID.UUID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("resolve owning organization for project issuer: %w", err)
		}
	}

	if organizationID != "" {
		if err := r.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
			OrganizationID:         organizationID,
			ProjectID:              orgProjectID(updated.ProjectID),
			Actor:                  urn.NewSystemPrincipal(IssuerMetadataRefreshActor),
			ActorDisplayName:       conv.PtrEmpty(IssuerMetadataRefreshActorDisplayName),
			ActorSlug:              nil,
			RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
			Slug:                   updated.Slug,
			IssuerURL:              updated.Issuer,
			Name:                   conv.FromPGText[string](updated.Name),
			SnapshotBefore:         mv.BuildRemoteSessionIssuerView(locked),
			SnapshotAfter:          mv.BuildRemoteSessionIssuerView(updated),
		}); err != nil {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("log remote session issuer update: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("commit transaction: %w", err)
	}

	// A global row, or a project row whose project is gone, has no organization feed to audit into.
	if organizationID == "" {
		logger.InfoContext(ctx, "remote session issuer metadata refreshed outside any organization feed",
			attr.SlogAuditAction("update"),
			attr.SlogAuditSubject("issuer"),
			attr.SlogAuditSubjectID(updated.ID.String()),
			attr.SlogOutcome(string(success)),
		)
	}
	return success, nil
}

func sameTimestamp(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid || a.InfinityModifier != b.InfinityModifier {
		return false
	}
	return !a.Valid || a.Time.Equal(b.Time)
}
