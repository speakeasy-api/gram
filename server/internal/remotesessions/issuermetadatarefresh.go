package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
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

	// issuerMetadataReactiveInterval is the least time between two reactive refreshes of one issuer; a visit inside it, successful or not, absorbs the request.
	issuerMetadataReactiveInterval = 10 * time.Minute

	// issuerMetadataRefreshSlots bounds the refreshes one replica runs at once; a use that finds no slot is skipped and the next use tries again.
	issuerMetadataRefreshSlots = 4

	// issuerMetadataRefreshBudget caps one detached refresh: discovery's own ten-second budget plus the writes.
	issuerMetadataRefreshBudget = 30 * time.Second
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

// issuerMetadataPlan is what one use of an issuer should do to its metadata; skipped, when set on an empty plan, is the outcome to record for doing nothing.
type issuerMetadataPlan struct {
	reproject bool
	fetch     bool
	skipped   remotesessionmetrics.IssuerMetadataRefreshOutcome
}

func (p issuerMetadataPlan) empty() bool { return !p.reproject && !p.fetch }

// issuerMetadataPlanner decides from a row snapshot what to do; it runs at flow time and again on the detached reload.
type issuerMetadataPlanner func(use IssuerMetadataUse, now time.Time) issuerMetadataPlan

// planIssuerMetadataRefresh: fetch when never visited, when the last visit (fetched_at or last_error_at) is stale, or when an outright transient failure is past its retry window; re-project when a stored document has a NULL capability column, no definitive failure stands, and no fetch is due (a fetch writes every column the re-projection would).
//
// An error stands only when it is newer than the last successful fetch. A partial read stamps both together, so its unread candidate waits for the daily cadence rather than the hourly retry.
func planIssuerMetadataRefresh(use IssuerMetadataUse, now time.Time) issuerMetadataPlan {
	visited, visitedAt := lastIssuerMetadataVisit(use)
	errorStands := use.MetadataLastErrorAt.Valid && (!use.MetadataFetchedAt.Valid || use.MetadataLastErrorAt.Time.After(use.MetadataFetchedAt.Time))
	transient := errorStands && use.MetadataLastErrorUrl.Valid && use.MetadataLastErrorUrl.String != ""
	definitiveFailure := errorStands && !transient

	fetch := !visited ||
		now.Sub(visitedAt) >= issuerMetadataStaleAfter ||
		(transient && now.Sub(use.MetadataLastErrorAt.Time) >= issuerMetadataRetryAfter)
	return issuerMetadataPlan{
		reproject: use.NeedsReprojection && !definitiveFailure && !fetch,
		fetch:     fetch,
		skipped:   "",
	}
}

// planReactiveIssuerMetadataRefresh: fetch regardless of the daily cadence, unless the last visit is inside the reactive interval.
func planReactiveIssuerMetadataRefresh(use IssuerMetadataUse, now time.Time) issuerMetadataPlan {
	if visited, visitedAt := lastIssuerMetadataVisit(use); visited && now.Sub(visitedAt) < issuerMetadataReactiveInterval {
		return issuerMetadataPlan{reproject: false, fetch: false, skipped: remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedRecent}
	}
	return issuerMetadataPlan{reproject: false, fetch: true, skipped: ""}
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
	metrics     *remotesessionmetrics.IssuerMetadataRefresh

	slots chan struct{}
	// inflight holds the issuer ids this replica is refreshing, so a burst of uses on one issuer fetches once here.
	inflight sync.Map

	// mu guards closed and every wg.Add: a use is admitted and registered in one step, so Shutdown cannot slip between them.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	// beforeAdmit runs just before a use is admitted; tests use it to hold a producer at the admission gate.
	beforeAdmit func()
}

// NewIssuerMetadataRefresher wires the on-use refresh. Two replicas may refresh one issuer at once; the row lock and timestamp compare in apply keep the writes consistent, so the duplicate costs one fetch.
func NewIssuerMetadataRefresher(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, policy *guardian.Policy, auditLogger *audit.Logger) *IssuerMetadataRefresher {
	return &IssuerMetadataRefresher{
		logger:      logger.With(attr.SlogComponent("remotesessions_issuer_metadata_refresh")),
		db:          db,
		policy:      policy,
		auditLogger: auditLogger,
		metrics:     remotesessionmetrics.NewIssuerMetadataRefresh(logger, meterProvider),
		slots:       make(chan struct{}, issuerMetadataRefreshSlots),
		inflight:    sync.Map{},
		mu:          sync.Mutex{},
		closed:      false,
		wg:          sync.WaitGroup{},
		beforeAdmit: nil,
	}
}

// NoteUse records that a session flow is using the issuer and, when its metadata is due, refreshes it off the request path. The caller keeps the stored row; the next use sees the result. Safe on a nil receiver.
func (r *IssuerMetadataRefresher) NoteUse(ctx context.Context, use IssuerMetadataUse) {
	r.admit(ctx, use, remotesessionmetrics.IssuerMetadataRefreshReasonOnUse, planIssuerMetadataRefresh)
}

// RequestRefresh refreshes the issuer's metadata because an upstream answer says the stored endpoints drifted, regardless of the daily cadence. A visit inside the reactive interval, at flow time or on the detached reload, absorbs the request as skipped_recent. Safe on a nil receiver.
func (r *IssuerMetadataRefresher) RequestRefresh(ctx context.Context, use IssuerMetadataUse, reason remotesessionmetrics.IssuerMetadataRefreshReason) {
	r.admit(ctx, use, reason, planReactiveIssuerMetadataRefresh)
}

// admit plans from the flow-time snapshot, takes a slot on the request path, and runs the work detached; the row is planned again from a fresh read before anything is written.
func (r *IssuerMetadataRefresher) admit(ctx context.Context, use IssuerMetadataUse, reason remotesessionmetrics.IssuerMetadataRefreshReason, plan issuerMetadataPlanner) {
	if r == nil {
		return
	}
	candidate := use.candidate()
	if p := plan(use, time.Now()); p.empty() {
		if p.skipped != "" {
			r.record(ctx, candidate.IssuerURL, reason, p.skipped)
		}
		return
	}
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(use.ID.String()), attr.SlogOAuthIssuer(use.IssuerURL), attr.SlogOAuthIssuerMetadataRefreshReason(reason))

	if _, running := r.inflight.LoadOrStore(use.ID, struct{}{}); running {
		r.record(ctx, candidate.IssuerURL, reason, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedInFlight)
		return
	}
	// The slot is taken on the request path so a busy replica answers at once; the work runs detached, so a client that disconnects mid-render still gets the refresh.
	select {
	case r.slots <- struct{}{}:
	default:
		r.inflight.Delete(use.ID)
		r.record(ctx, candidate.IssuerURL, reason, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedBusy)
		return
	}

	if r.beforeAdmit != nil {
		r.beforeAdmit()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		<-r.slots
		r.inflight.Delete(use.ID)
		r.record(ctx, candidate.IssuerURL, reason, remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedShutdown)
		return
	}
	r.wg.Add(1)
	r.mu.Unlock()

	// Only the trace carries over: the request's authenticated actor and OAuth client must not reach the audit entry, which is the system's.
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		defer r.wg.Done()
		// LIFO: the in-flight entry drops before the slot frees, so a use that finds a free slot never sees a stale entry.
		defer func() { <-r.slots }()
		defer r.inflight.Delete(use.ID)
		ctx, cancel := context.WithTimeout(detached, issuerMetadataRefreshBudget)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(ctx, "issuer metadata refresh panicked", attr.SlogError(fmt.Errorf("%v", rec)))
				r.record(ctx, candidate.IssuerURL, reason, remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError)
			}
		}()

		// The flow-time snapshot may be stale by now: another visit may have refreshed or failed since, so plan again from the row.
		existing, outcome, err := r.load(ctx, candidate)
		if err != nil || outcome != "" {
			r.record(ctx, candidate.IssuerURL, reason, outcome)
			if err != nil {
				logger.ErrorContext(ctx, "reload issuer metadata before refresh", attr.SlogError(err))
			}
			return
		}
		p := plan(issuerMetadataUseFromRow(existing), time.Now())
		if p.empty() {
			if p.skipped != "" {
				r.record(ctx, candidate.IssuerURL, reason, p.skipped)
			}
			return
		}
		r.run(ctx, logger, existing, p, reason)
	}()
}

// tokenEndpointMissing reports a token endpoint answer that says the stored endpoint is gone rather than that the grant was refused.
func tokenEndpointMissing(statusCode int) bool {
	return statusCode == http.StatusNotFound || statusCode == http.StatusGone
}

// noteTokenEndpointMissing requests a reactive refresh when the stored token endpoint answered 404 or 410; the caller's error is untouched.
func noteTokenEndpointMissing(ctx context.Context, r *IssuerMetadataRefresher, client repo.GetRemoteSessionClientWithIssuerByIDRow, statusCode int) {
	if tokenEndpointMissing(statusCode) {
		r.RequestRefresh(ctx, issuerUseFromClientRow(client), remotesessionmetrics.IssuerMetadataRefreshReasonTokenEndpointMissing)
	}
}

// noteUnknownSigningKey requests a reactive refresh when an ID token named a kid the key set lacks even after the forced key refresh: the jwks_uri itself may have moved. Signature and claim failures are not drift.
func noteUnknownSigningKey(ctx context.Context, r *IssuerMetadataRefresher, client repo.GetRemoteSessionClientWithIssuerByIDRow, err error) {
	if errors.Is(err, jwks.ErrKeyNotFound) {
		r.RequestRefresh(ctx, issuerUseFromClientRow(client), remotesessionmetrics.IssuerMetadataRefreshReasonUnknownSigningKey)
	}
}

// Wait blocks until every refresh NoteUse started has finished; later uses may still start more.
func (r *IssuerMetadataRefresher) Wait() {
	if r == nil {
		return
	}
	r.wg.Wait()
}

// Shutdown refuses every later use and waits for the refreshes in flight; call it before the database closes. Safe on a nil receiver.
func (r *IssuerMetadataRefresher) Shutdown() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.wg.Wait()
}

// run does the one thing the plan asks for: a fetch writes every column a re-projection would, so the two never run in one visit.
func (r *IssuerMetadataRefresher) run(ctx context.Context, logger *slog.Logger, existing repo.RemoteSessionIssuer, plan issuerMetadataPlan, reason remotesessionmetrics.IssuerMetadataRefreshReason) {
	switch {
	case plan.fetch:
		if _, err := r.refresh(ctx, existing, reason); err != nil {
			logger.ErrorContext(ctx, "refresh issuer metadata", attr.SlogError(err))
		}
	case plan.reproject:
		if _, err := r.reproject(ctx, existing, reason); err != nil {
			logger.ErrorContext(ctx, "reproject issuer metadata", attr.SlogError(err))
		}
	}
}

// reproject fills an issuer's NULL capability columns from its stored document, leaving set columns, the document, and the tracking columns alone.
func (r *IssuerMetadataRefresher) reproject(ctx context.Context, existing repo.RemoteSessionIssuer, reason remotesessionmetrics.IssuerMetadataRefreshReason) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer), attr.SlogOAuthIssuerMetadataRefreshReason(reason))

	doc, err := decodeStoredIssuerDocument(existing)
	if err != nil {
		logger.WarnContext(ctx, "stored issuer metadata document cannot be re-projected", attr.SlogError(err))
		msg := "stored issuer metadata document could not be decoded"
		if ude, ok := errors.AsType[*untrustedDocumentError](err); ok {
			msg = ude.reason
		}
		outcome, err := r.recordReprojectionFailure(ctx, existing, msg)
		return r.record(ctx, existing.Issuer, reason, outcome), err
	}

	outcome, err := r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.ReprojectRemoteSessionIssuerMetadataCapabilities(ctx, repo.ReprojectRemoteSessionIssuerMetadataCapabilitiesParams{
			CodeChallengeMethodsSupported:              orEmptySlice(doc.CodeChallengeMethodsSupported),
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
	return r.record(ctx, existing.Issuer, reason, outcome), err
}

// refresh fetches an issuer's upstream metadata and applies it; discovery failures are outcomes, not errors.
func (r *IssuerMetadataRefresher) refresh(ctx context.Context, existing repo.RemoteSessionIssuer, reason remotesessionmetrics.IssuerMetadataRefreshReason) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer), attr.SlogOAuthIssuerMetadataRefreshReason(reason))

	params, _, err := refreshIssuerMetadata(ctx, r.policy, existing)
	if err != nil {
		msg, _ := discoveryFailureMessage(err)
		retryURL := discoveryRetryURL(err)
		failure := remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure
		if retryURL != "" {
			failure = remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure
		}
		logger.WarnContext(ctx, "issuer metadata refresh failed", attr.SlogOutcome(string(failure)), attr.SlogError(err))
		outcome, err := r.recordFailure(ctx, existing, msg, retryURL, failure)
		return r.record(ctx, existing.Issuer, reason, outcome), err
	}

	success := remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed
	if params.MetadataLastErrorUrl != "" {
		success = remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshedPartial
	}
	outcome, err := r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	}, success)
	return r.record(ctx, existing.Issuer, reason, outcome), err
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

func issuerMetadataUseFromRow(row repo.RemoteSessionIssuer) IssuerMetadataUse {
	return IssuerMetadataUse{
		ID:                   row.ID,
		IssuerURL:            row.Issuer,
		ProjectID:            row.ProjectID,
		OrganizationID:       row.OrganizationID,
		MetadataFetchedAt:    row.MetadataFetchedAt,
		MetadataLastErrorAt:  row.MetadataLastErrorAt,
		MetadataLastErrorUrl: row.MetadataLastErrorUrl,
		NeedsReprojection: len(row.Metadata) > 0 && (row.IntrospectionEndpointAuthMethodsSupported == nil ||
			row.IDTokenSigningAlgValuesSupported == nil ||
			row.ClaimsSupported == nil ||
			!row.BackchannelLogoutSupported.Valid ||
			!row.AuthorizationResponseIssParameterSupported.Valid ||
			row.CodeChallengeMethodsSupported == nil),
	}
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

func (r *IssuerMetadataRefresher) record(ctx context.Context, issuerURL string, reason remotesessionmetrics.IssuerMetadataRefreshReason, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) remotesessionmetrics.IssuerMetadataRefreshOutcome {
	r.metrics.Record(ctx, issuerURL, reason, outcome)
	return outcome
}

// recordFailure stamps the error trio on the row as the refresh read it; a newer write, fetch or failure, wins. A retry URL marks the failure transient.
func (r *IssuerMetadataRefresher) recordFailure(ctx context.Context, existing repo.RemoteSessionIssuer, msg, retryURL string, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	rows, err := repo.New(r.db).RecordRemoteSessionIssuerMetadataRefreshFailure(ctx, repo.RecordRemoteSessionIssuerMetadataRefreshFailureParams{
		MetadataLastError:         msg,
		MetadataLastErrorUrl:      retryURL,
		ObservedMetadataFetchedAt: existing.MetadataFetchedAt,
		ObservedUpdatedAt:         existing.UpdatedAt,
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

// recordReprojectionFailure preserves a pending upstream error's timestamp and retry URL; otherwise it stamps a new definitive failure.
func (r *IssuerMetadataRefresher) recordReprojectionFailure(ctx context.Context, existing repo.RemoteSessionIssuer, msg string) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	rows, err := repo.New(r.db).RecordRemoteSessionIssuerMetadataReprojectionFailure(ctx, repo.RecordRemoteSessionIssuerMetadataReprojectionFailureParams{
		MetadataLastError:         msg,
		ObservedMetadataFetchedAt: existing.MetadataFetchedAt,
		ObservedUpdatedAt:         existing.UpdatedAt,
		ID:                        existing.ID,
		Issuer:                    existing.Issuer,
		ProjectID:                 existing.ProjectID,
		OrganizationID:            existing.OrganizationID,
	})
	if err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("record issuer metadata reprojection failure: %w", err)
	}
	if rows == 0 {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
	}
	return remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojectInvalid, nil
}

// apply runs the write and, when the row's view changed, the system-actor audit entry in one transaction, snapshotting the row under lock. A fetch that restates what is stored moves only the tracking columns and is not audited.
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

	before, after := mv.BuildRemoteSessionIssuerView(locked), mv.BuildRemoteSessionIssuerView(updated)
	if organizationID != "" && issuerViewChanged(before, after) {
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
			SnapshotBefore:         before,
			SnapshotAfter:          after,
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

// issuerViewChanged compares the audited views ignoring updated_at, which every write moves; the view carries none of the tracking columns.
func issuerViewChanged(before, after *types.RemoteSessionIssuer) bool {
	b, a := *before, *after
	b.UpdatedAt, a.UpdatedAt = "", ""
	return !reflect.DeepEqual(b, a)
}

func sameTimestamp(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid || a.InfinityModifier != b.InfinityModifier {
		return false
	}
	return !a.Valid || a.Time.Equal(b.Time)
}
