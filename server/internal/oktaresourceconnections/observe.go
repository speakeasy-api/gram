package oktaresourceconnections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ObserverActor is the system principal observation audit entries carry.
const (
	ObserverActor            = "okta-resource-connection-observer"
	ObserverActorDisplayName = "Cross App Access exchange"
)

const (
	// rollbackTimeout bounds a rollback after the observation's deadline.
	rollbackTimeout = time.Second

	// refreshWindow skips rewriting an unchanged result, and holds any failure
	// back from replacing verified, for this long after the recorded attempt.
	refreshWindow = 5 * time.Minute

	// verifiedHold keeps a failure that may be one user's from replacing
	// verified until no exchange has succeeded for this long.
	verifiedHold = 24 * time.Hour

	meterObservations = "gram.okta_resource_connection.observations"
)

// Dispositions of an observed attempt, for the observations counter.
const (
	dispositionIgnored       = "ignored"
	dispositionNotApplicable = "not_applicable"
	dispositionSuperseded    = "superseded"
	dispositionLocked        = "locked"
	dispositionUnchanged     = "unchanged"
	dispositionChanged       = "changed"
	dispositionError         = "error"
)

// Observer records identity chaining exchange results on the organization's
// confirmed Okta resource connections.
type Observer struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	audit        *audit.Logger
	observations metric.Int64Counter
}

var _ identitychaining.Observer = (*Observer)(nil)

func NewObserver(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, auditLogger *audit.Logger) *Observer {
	logger = logger.With(attr.SlogComponent("oktaresourceconnections"))
	observations, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/oktaresourceconnections").Int64Counter(
		meterObservations,
		metric.WithDescription("Identity chaining attempts seen by the Okta resource connection observer, by disposition and result."),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterObservations), attr.SlogError(err))
	}
	return &Observer{logger: logger, db: db, audit: auditLogger, observations: observations}
}

// resultFor maps an attempt to what it shows about the Okta resource
// connection. Only the identity provider's own exchange errors speak to the
// connection; the resource authorization server speaks only after a validated
// ID-JAG, only about itself, and only when it answered. Ambiguous, per-user,
// local and transient outcomes show nothing.
func resultFor(o identitychaining.Observation) (Result, bool) {
	out := o.Outcome
	switch {
	case out.Succeeded():
		return ResultVerified, o.GrantValidated
	case out.Stage == identitychaining.StageRedemption:
		return ResultDownstreamRejected, o.GrantValidated && !out.Retryable && providerRejection(out.Reason)
	case out.Stage != identitychaining.StageExchange:
		return "", false
	}
	switch out.Reason {
	case identitychaining.ReasonInvalidTarget:
		return ResultConnectionMissing, true
	case identitychaining.ReasonScopePolicyDenied:
		return ResultScopeNotAllowed, true
	case identitychaining.ReasonInvalidClient:
		return ResultClientAuthFailed, true
	default:
		return "", false
	}
}

// providerRejection reports reasons only a provider's error response yields.
// access_denied is left out: it names one user, not the resource's trust.
func providerRejection(r identitychaining.Reason) bool {
	switch r {
	case identitychaining.ReasonInvalidGrant, identitychaining.ReasonInvalidClient,
		identitychaining.ReasonInvalidTarget, identitychaining.ReasonScopePolicyDenied, identitychaining.ReasonUnknownRejection,
		identitychaining.ReasonInsufficientScope:
		return true
	default:
		return false
	}
}

// supersedes reports whether result, from an attempt that started at
// startedAt, replaces what rc records. Newer results normally win, so an older
// contradictory attempt cannot land over them, with two throttles:
//   - An unchanged result is rewritten at most once per refreshWindow, so every
//     fresh token does not write to the shared row.
//   - A failure replaces verified only after refreshWindow, and one that can
//     be a single user's (scope_not_allowed, downstream_rejected) only after
//     verifiedHold without a success, so mixed users do not flap the row.
func supersedes(rc repo.OktaResourceConnection, result Result, startedAt time.Time) bool {
	if !startedAt.After(rc.UpdatedAt.Time) {
		return false
	}
	if !rc.ObservedAt.Valid {
		return true
	}
	age := startedAt.Sub(rc.ObservedAt.Time)
	previous := Result(rc.ObservedResult.String)
	switch {
	case age <= 0:
		return false
	case previous == result:
		return age >= refreshWindow
	case previous == ResultVerified && (result == ResultScopeNotAllowed || result == ResultDownstreamRejected):
		return age >= verifiedHold
	case previous == ResultVerified:
		return age >= refreshWindow
	default:
		return true
	}
}

// applies reports whether result may be recorded on rc at all. invalid_target
// for another audience does not establish that the confirmed connection is
// missing; static readiness explains the mismatch.
func applies(rc repo.OktaResourceConnection, result Result, remoteIssuer string) bool {
	return result != ResultConnectionMissing || remotesessions.IssuerURLsEqual(rc.Audience, remoteIssuer)
}

// ObserveAttempt records the attempt's result on the confirmed resource
// connection it was for. Every row identity comes from the executor's
// server-side selection and the organization's live connection.
func (o *Observer) ObserveAttempt(ctx context.Context, obs identitychaining.Observation) error {
	result, ok := resultFor(obs)
	if !ok {
		o.count(ctx, dispositionIgnored, "")
		return nil
	}
	disposition, err := o.observe(ctx, obs, result)
	if err != nil {
		disposition = dispositionError
	}
	o.count(ctx, disposition, result)
	return err
}

func (o *Observer) count(ctx context.Context, disposition string, result Result) {
	if o.observations == nil {
		return
	}
	o.observations.Add(ctx, 1, metric.WithAttributes(attr.Outcome(disposition), attr.Reason(string(result))))
}

// observe records result under the row lock and reports what happened. Skipping
// a row another observer or a confirmation holds is best-effort by design: the
// next fresh attempt for that upstream records it.
func (o *Observer) observe(ctx context.Context, obs identitychaining.Observation, result Result) (string, error) {
	// Audit as the system, not the proxied human or agent in the request.
	deadline, hasDeadline := ctx.Deadline()
	detached := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	detached = contextvalues.SetActingSurface(detached, string(audit.SurfaceSystem))
	if hasDeadline {
		var cancel context.CancelFunc
		detached, cancel = context.WithDeadline(detached, deadline)
		defer cancel()
	}
	ctx = detached

	q := repo.New(o.db)
	conn, err := q.GetLiveConnection(ctx, obs.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dispositionNotApplicable, nil
	}
	if err != nil {
		return "", fmt.Errorf("load identity provider connection: %w", err)
	}
	// The exchange went to another identity provider, or the connection is
	// not one Confirm accepts.
	if conn.RemoteSessionIssuerID != obs.TrustedIssuerID || (conn.Status != "verified" && conn.Status != "degraded") {
		return dispositionNotApplicable, nil
	}
	rc, err := q.GetResourceConnection(ctx, repo.GetResourceConnectionParams{
		OrganizationID:               obs.OrganizationID,
		IdentityProviderConnectionID: conn.ID,
		RemoteSessionIssuerID:        obs.RemoteIssuerID,
		Resource:                     strings.TrimRight(obs.Resource, "/"),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dispositionNotApplicable, nil
	}
	if err != nil {
		return "", fmt.Errorf("read resource connection: %w", err)
	}
	// Unlocked first read: most attempts repeat a recent result and stop here.
	if !applies(rc, result, obs.RemoteIssuer) {
		return dispositionNotApplicable, nil
	}
	if !supersedes(rc, result, obs.StartedAt) {
		return dispositionSuperseded, nil
	}

	dbtx, err := o.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin observation: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		return dbtx.Rollback(rollbackCtx)
	})
	qtx := repo.New(dbtx)
	locked, err := qtx.GetResourceConnectionForObservation(ctx, repo.GetResourceConnectionForObservationParams{ID: rc.ID, OrganizationID: obs.OrganizationID})
	// Held by a concurrent observation or confirmation, or gone.
	if errors.Is(err, pgx.ErrNoRows) {
		return dispositionLocked, nil
	}
	if err != nil {
		return "", fmt.Errorf("lock resource connection: %w", err)
	}
	if !applies(locked, result, obs.RemoteIssuer) {
		return dispositionNotApplicable, nil
	}
	if !supersedes(locked, result, obs.StartedAt) {
		return dispositionSuperseded, nil
	}
	n, err := qtx.RecordObservation(ctx, repo.RecordObservationParams{
		ObservedResult: conv.ToPGText(string(result)),
		ObservedAt:     conv.ToPGTimestamptz(obs.StartedAt),
		ID:             locked.ID,
		OrganizationID: obs.OrganizationID,
	})
	if err != nil {
		return "", fmt.Errorf("record observation: %w", err)
	}
	if n == 0 {
		return dispositionSuperseded, nil
	}

	previous := Result(locked.ObservedResult.String)
	if previous != result {
		agentRecorded := conn.AgentID.Valid && conn.AgentID.String != ""
		after := locked
		after.ObservedResult = conv.ToPGText(string(result))
		if err := o.audit.LogOktaResourceConnectionObserve(ctx, dbtx, audit.LogOktaResourceConnectionEvent{
			OrganizationID:        obs.OrganizationID,
			ProjectID:             uuid.Nil,
			Actor:                 urn.NewSystemPrincipal(ObserverActor),
			ActorDisplayName:      conv.PtrEmpty(ObserverActorDisplayName),
			ActorSlug:             nil,
			ResourceConnectionURN: urn.NewOktaResourceConnection(locked.ID),
			ServerName:            locked.Resource,
			ServerSlug:            "",
			SnapshotBefore:        observedSnapshot(locked, obs.RemoteIssuer, agentRecorded),
			SnapshotAfter:         observedSnapshot(after, obs.RemoteIssuer, agentRecorded),
		}); err != nil {
			return "", fmt.Errorf("audit observation: %w", err)
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit observation: %w", err)
	}
	if previous == result {
		return dispositionUnchanged, nil
	}
	o.logger.InfoContext(ctx, "okta resource connection observation changed",
		attr.SlogOrganizationID(obs.OrganizationID),
		attr.SlogRemoteSessionIssuerID(obs.RemoteIssuerID.String()),
		attr.SlogOAuthResource(locked.Resource),
		attr.SlogOutcome(string(result)),
	)
	return dispositionChanged, nil
}

// observedSnapshot is an audit snapshot of a confirmed resource connection
// whose upstream is known to advertise ID-JAG, since an exchange ran for it.
func observedSnapshot(rc repo.OktaResourceConnection, remoteIssuer string, agentRecorded bool) *audit.OktaResourceConnectionSnapshot {
	return &audit.OktaResourceConnectionSnapshot{
		ConnectionID:      rc.IdentityProviderConnectionID.String(),
		IssuerID:          rc.RemoteSessionIssuerID.String(),
		Resource:          rc.Resource,
		Audience:          rc.Audience,
		OktaApplicationID: rc.OktaApplicationID.String,
		ObservedResult:    rc.ObservedResult.String,
		State: string(Derive(Inputs{
			AdvertisesIDJAG:  true,
			AgentRecorded:    agentRecorded,
			Confirmed:        true,
			AudienceMismatch: !remotesessions.IssuerURLsEqual(rc.Audience, remoteIssuer),
			Observed:         Result(rc.ObservedResult.String),
		})),
	}
}
