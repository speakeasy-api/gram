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

// rollbackTimeout bounds a rollback after the observation's deadline.
const rollbackTimeout = time.Second

// Observer records identity chaining exchange results on the organization's
// confirmed Okta resource connections.
type Observer struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	audit  *audit.Logger
}

var _ identitychaining.Observer = (*Observer)(nil)

func NewObserver(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger) *Observer {
	return &Observer{
		logger: logger.With(attr.SlogComponent("oktaresourceconnections")),
		db:     db,
		audit:  auditLogger,
	}
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
func providerRejection(r identitychaining.Reason) bool {
	switch r {
	case identitychaining.ReasonInvalidGrant, identitychaining.ReasonAccessDenied, identitychaining.ReasonInvalidClient,
		identitychaining.ReasonInvalidTarget, identitychaining.ReasonScopePolicyDenied, identitychaining.ReasonUnknownRejection,
		identitychaining.ReasonInsufficientScope:
		return true
	default:
		return false
	}
}

// supersedes reports whether result, from an attempt that started at
// startedAt, replaces what rc records. Newer unchanged results also advance
// observed_at so older contradictory attempts cannot replace them.
func supersedes(rc repo.OktaResourceConnection, _ Result, startedAt time.Time) bool {
	if !startedAt.After(rc.UpdatedAt.Time) {
		return false
	}
	if !rc.ObservedAt.Valid {
		return true
	}
	return startedAt.After(rc.ObservedAt.Time)
}

// ObserveAttempt records the attempt's result on the confirmed resource
// connection it was for. Every row identity comes from the executor's
// server-side selection and the organization's live connection.
func (o *Observer) ObserveAttempt(ctx context.Context, obs identitychaining.Observation) error {
	result, ok := resultFor(obs)
	if !ok {
		return nil
	}
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
		return nil
	}
	if err != nil {
		return fmt.Errorf("load identity provider connection: %w", err)
	}
	// The exchange went to another identity provider, or the connection is
	// not one Confirm accepts.
	if conn.RemoteSessionIssuerID != obs.TrustedIssuerID || (conn.Status != "verified" && conn.Status != "degraded") {
		return nil
	}
	rc, err := q.GetResourceConnection(ctx, repo.GetResourceConnectionParams{
		OrganizationID:               obs.OrganizationID,
		IdentityProviderConnectionID: conn.ID,
		RemoteSessionIssuerID:        obs.RemoteIssuerID,
		Resource:                     strings.TrimRight(obs.Resource, "/"),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read resource connection: %w", err)
	}
	// invalid_target for another audience does not establish that the
	// confirmed connection is missing; static readiness explains the mismatch.
	if result == ResultConnectionMissing && !remotesessions.IssuerURLsEqual(rc.Audience, obs.RemoteIssuer) {
		return nil
	}
	if !supersedes(rc, result, obs.StartedAt) {
		return nil
	}

	dbtx, err := o.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin observation: %w", err)
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
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock resource connection: %w", err)
	}
	// invalid_target for another audience does not establish that the
	// confirmed connection is missing; static readiness explains the mismatch.
	if result == ResultConnectionMissing && !remotesessions.IssuerURLsEqual(locked.Audience, obs.RemoteIssuer) {
		return nil
	}
	if !supersedes(locked, result, obs.StartedAt) {
		return nil
	}
	n, err := qtx.RecordObservation(ctx, repo.RecordObservationParams{
		ObservedResult: conv.ToPGText(string(result)),
		ObservedAt:     conv.ToPGTimestamptz(obs.StartedAt),
		ID:             locked.ID,
		OrganizationID: obs.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("record observation: %w", err)
	}
	if n == 0 {
		return nil
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
			return fmt.Errorf("audit observation: %w", err)
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit observation: %w", err)
	}
	if previous != result {
		o.logger.InfoContext(ctx, "okta resource connection observation changed",
			attr.SlogOrganizationID(obs.OrganizationID),
			attr.SlogRemoteSessionIssuerID(obs.RemoteIssuerID.String()),
			attr.SlogOAuthResource(locked.Resource),
			attr.SlogOutcome(string(result)),
		)
	}
	return nil
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
