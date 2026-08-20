// upstreamrevoke.go is the client side of RFC 7009. Gram already implements the
// *server* side — internal/mcp/authnchallenge_revoke.go answers /revoke for the
// MCP clients Gram issues tokens to. This file is the mirror image: telling the
// upstream authorization server that a Remote Session's credentials are dead.
//
// Without it, revoking a Remote Session is local-only. The row is soft-deleted
// and Gram stops presenting the token, but the upstream still holds a live
// access/refresh pair that keeps working anywhere else it is presented until it
// expires on its own clock — which, for a refresh token, can be months.
//
// Everything here is best-effort by construction. The local revoke is the
// security control the caller asked for and it has already committed by the
// time any of this runs; the upstream call is an improvement on top of it, made
// against a third party Gram does not own and cannot make promises about. A
// revoke that reports failure because someone else's server was slow would be
// both wrong and unactionable, so failures are logged and metered, never
// returned.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// upstreamRevokeTimeout bounds one RFC 7009 POST. Deliberately tighter than
	// the refresh path's budget: a refresh has to succeed for the session to
	// keep working, so it is worth waiting on, whereas a revocation that does
	// not land costs nothing beyond the upstream token living out its natural
	// lifetime. The caller is already blocked post-commit, so the shorter the
	// better.
	upstreamRevokeTimeout = 5 * time.Second

	// bulkRevokeTimeout bounds a whole batch, however many sessions it holds.
	// Revoking every session on a client is an operator action performed from a
	// dashboard that is waiting on the response, so the batch cannot be allowed
	// to grow a deadline proportional to its size. Sessions still unattempted
	// when it expires are counted and logged rather than silently dropped.
	bulkRevokeTimeout = 30 * time.Second

	// bulkRevokeConcurrency caps in-flight POSTs within one batch. Every session
	// on a client shares that client's issuer, so the whole batch lands on a
	// single upstream host and an unbounded fan-out would read as a burst from
	// Gram against one customer's identity provider.
	bulkRevokeConcurrency = 8
)

// UpstreamRevoker pushes RFC 7009 revocations to the issuers Gram holds Remote
// Session tokens against.
//
// It is a separate type from Service rather than a set of methods on it so the
// bulk and cascade paths — which do not run inside a Goa handler — can hold one
// without dragging the whole service in, and so tests can drive it directly.
type UpstreamRevoker struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	enc    *encryption.Client

	// client is built once and shared by every revocation. Guardian's pooled
	// transport is meant for exactly this — a long-lived client making repeated
	// requests to the same hosts — and a bulk revoke is the case it pays off on,
	// since every session in a batch shares one issuer. Constructing one per
	// call instead would open a connection per session and hold each idle until
	// it timed out, which is the file-descriptor leak PooledClient's own
	// documentation warns against.
	client *guardian.HTTPClient

	metrics *remotesessionmetrics.Revoke
}

func NewUpstreamRevoker(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy) *UpstreamRevoker {
	logger = logger.With(attr.SlogComponent("remote-session-upstream-revoke"))
	return &UpstreamRevoker{
		logger:  logger,
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotesessions"),
		db:      db,
		enc:     enc,
		client:  policy.PooledClient(),
		metrics: remotesessionmetrics.NewRevoke(logger, meterProvider),
	}
}

// RevokedCredentials is the minimum an upstream revocation needs from a session
// that has just been soft-deleted: which client the grant belongs to, and the
// stored token material.
//
// Deliberately not repo.RemoteSession. The three revoke paths return three
// different row shapes (the project handler returns the table row, the
// organization handler returns it flattened alongside the client's project id,
// and the bulk delete returns its own projection), and none of them is more
// canonical than the others. Naming the three fields the revocation actually
// reads keeps every caller honest about what it is handing over.
type RevokedCredentials struct {
	// RemoteSessionClientID identifies the client whose registration and issuer
	// supply the revocation endpoint and the client authentication to use.
	RemoteSessionClientID uuid.UUID

	// AccessTokenEncrypted is the session's stored access token, sent only when
	// no refresh token is stored.
	AccessTokenEncrypted string

	// RefreshTokenEncrypted is the session's stored refresh token, preferred
	// over the access token when present. Invalid when the grant never issued
	// one or a prior invalid_grant cleared it.
	RefreshTokenEncrypted pgtype.Text
}

// revokedCredentials adapts the rows returned by a bulk soft-delete into the
// shape the revoker consumes.
func revokedCredentials(rows []repo.SoftDeleteRemoteSessionsByClientIDRow) []RevokedCredentials {
	creds := make([]RevokedCredentials, 0, len(rows))
	for _, row := range rows {
		creds = append(creds, RevokedCredentials{
			RemoteSessionClientID: row.RemoteSessionClientID,
			AccessTokenEncrypted:  row.AccessTokenEncrypted,
			RefreshTokenEncrypted: row.RefreshTokenEncrypted,
		})
	}
	return creds
}

// SoftDeleteSubjectSessions tombstones every upstream grant a subject holds
// through one user session issuer, inside the caller's transaction, and returns
// the credentials to hand to [UpstreamRevoker.RevokeAllDetached] once that
// transaction commits.
//
// Split in two on purpose. The tombstone belongs in the caller's transaction so
// it commits or rolls back with the revocation that triggered it; the upstream
// POSTs must not, because they are network round trips to a third party and
// holding a pooled connection open for them — or worse, rolling back after
// asking a provider to destroy a token — is exactly what the post-commit split
// exists to prevent.
//
// Takes a DBTX rather than a transaction type so callers in other packages can
// pass whichever handle their own transaction gave them.
func (r *UpstreamRevoker) SoftDeleteSubjectSessions(ctx context.Context, tx repo.DBTX, subject urn.SessionSubject, userSessionIssuerID uuid.UUID, projectID uuid.UUID) ([]RevokedCredentials, error) {
	rows, err := repo.New(tx).SoftDeleteRemoteSessionsBySubjectAndUserSessionIssuer(ctx, repo.SoftDeleteRemoteSessionsBySubjectAndUserSessionIssuerParams{
		SubjectUrn:          subject,
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("soft delete remote sessions for subject: %w", err)
	}

	creds := make([]RevokedCredentials, 0, len(rows))
	for _, row := range rows {
		creds = append(creds, RevokedCredentials{
			RemoteSessionClientID: row.RemoteSessionClientID,
			AccessTokenEncrypted:  row.AccessTokenEncrypted,
			RefreshTokenEncrypted: row.RefreshTokenEncrypted,
		})
	}
	return creds, nil
}

// RevokeDetached runs the upstream revocation for an already-soft-deleted
// session, off the caller's cancellation and under its own deadline.
//
// Call it *after* the local transaction commits. Two reasons, and both are
// load-bearing:
//
//   - The POST is a network round trip to a third party. Running it inside the
//     transaction would hold a pooled connection open for its duration, and a
//     rollback would then leave a live local session whose upstream token Gram
//     had already asked to have destroyed.
//   - The row is already committed as deleted, so a caller that disconnects
//     between the commit and this call must not take the revocation with it.
//     context.WithoutCancel is what keeps a closed browser tab from leaving the
//     upstream token live.
//
// Same shape as the post-commit jti push in internal/usersessions, with one
// deliberate difference: that one returns an error when the push fails, because
// a still-valid Gram-issued JWT is Gram's own security control failing. Here the
// dependency is someone else's authorization server, so the result is recorded
// and the caller is told nothing.
func (r *UpstreamRevoker) RevokeDetached(ctx context.Context, cred RevokedCredentials) {
	revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamRevokeTimeout)
	defer cancel()

	r.revoke(revokeCtx, cred)
}

// RevokeAllDetached runs upstream revocations for a batch of sessions that have
// already been soft-deleted together — every session on a client, whether the
// operator revoked them explicitly or deleted the client out from under them.
//
// Post-commit and off the caller's cancellation for the same reasons as
// RevokeDetached. Two bounds on top of that, because a batch is unbounded in a
// way a single revoke is not:
//
//   - bulkRevokeConcurrency in flight at once. The batch shares one issuer, so
//     the fan-out is aimed at a single upstream host.
//   - bulkRevokeTimeout across the whole batch, with each session still holding
//     its own upstreamRevokeTimeout inside it. Without the per-session bound one
//     unresponsive upstream would hold a concurrency slot for the entire batch
//     budget and starve the sessions behind it.
//
// A batch that exhausts the budget leaves its remainder unattempted. Those are
// counted, logged, and metered as dropped rather than passed off as done: the
// local revoke is complete either way, but an operator reading "revoked" should
// not have to guess whether the upstream heard about it.
func (r *UpstreamRevoker) RevokeAllDetached(ctx context.Context, creds []RevokedCredentials) {
	if len(creds) == 0 {
		return
	}

	batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bulkRevokeTimeout)
	defer cancel()

	// Parents the per-session spans, so a batch reads as one unit in a trace
	// rather than as a scatter of unrelated POSTs, and so the wall-clock the
	// caller is blocked for is attributable to the batch rather than inferred
	// from the widest child.
	batchCtx, span := r.tracer.Start(batchCtx, "remote_session.upstream_revoke_batch")
	defer span.End()
	span.SetAttributes(attr.OAuthProviderCount(len(creds)))

	// A plain Group rather than errgroup.WithContext: revoke never returns an
	// error (it records outcomes instead), so there is no first-error to
	// propagate and cancelling siblings on one failure would be wrong here —
	// each session's revocation is independent of the others.
	group := &errgroup.Group{}
	group.SetLimit(bulkRevokeConcurrency)

	var attempted atomic.Int64
	for _, cred := range creds {
		group.Go(func() error {
			if batchCtx.Err() != nil {
				return nil
			}
			attempted.Add(1)

			revokeCtx, cancelOne := context.WithTimeout(batchCtx, upstreamRevokeTimeout)
			defer cancelOne()

			r.revoke(revokeCtx, cred)
			return nil
		})
	}
	_ = group.Wait()

	dropped := len(creds) - int(attempted.Load())
	if dropped <= 0 {
		return
	}

	r.logger.WarnContext(ctx, "upstream revoke: batch budget exhausted before every session was attempted",
		attr.SlogRemoteSessionClientID(creds[0].RemoteSessionClientID.String()),
		attr.SlogRemoteSessionRevokeDroppedCount(dropped),
	)
	span.SetAttributes(attr.RemoteSessionRevokeDroppedCount(dropped))
	for range dropped {
		r.metrics.Record(ctx, "", remotesessionmetrics.RevokeOutcomeDropped)
	}
}

// revoke performs the whole sequence for one session and reports exactly one
// outcome, on both a span and the metric. Split from RevokeDetached so the bulk
// path can drive it directly under the batch's own budget and concurrency limit.
func (r *UpstreamRevoker) revoke(ctx context.Context, cred RevokedCredentials) {
	ctx, span := r.tracer.Start(ctx, "remote_session.upstream_revoke")
	defer span.End()

	issuerURL, outcome := r.revokeOnce(ctx, cred)

	// Reported in one place so the span and the metric can never disagree about
	// how a revocation ended. Without a span the revocation is only visible in a
	// trace as a bare HTTP POST, which is indistinguishable from a token
	// exchange whenever an issuer advertises one URL for both endpoints.
	span.SetAttributes(attr.OAuthIssuer(issuerURL), attr.Outcome(outcome))
	r.metrics.Record(ctx, issuerURL, outcome)
}

// revokeOnce runs the sequence and reports where it stopped. The returned
// issuer URL attributes the outcome, and is empty when the revocation failed
// before any issuer could be identified.
func (r *UpstreamRevoker) revokeOnce(ctx context.Context, cred RevokedCredentials) (issuerURL string, outcome remotesessionmetrics.RevokeOutcome) {
	logger := r.logger.With(
		attr.SlogRemoteSessionClientID(cred.RemoteSessionClientID.String()),
	)

	client, err := repo.New(r.db).GetRemoteSessionClientRevocationTargetByID(ctx, cred.RemoteSessionClientID)
	if err != nil {
		// The lookup tolerates soft-deleted clients and issuers, so no rows means
		// the row is gone outright — a hard delete racing the revoke. Nothing is
		// addressable without it and nothing is recoverable.
		if errors.Is(err, pgx.ErrNoRows) {
			return "", remotesessionmetrics.RevokeOutcomeSkipped
		}
		logger.WarnContext(ctx, "upstream revoke: could not load client", attr.SlogError(err))
		return "", remotesessionmetrics.RevokeOutcomeInternal
	}

	logger = logger.With(
		attr.SlogOAuthIssuer(client.IssuerUrl),
		attr.SlogRemoteSessionIssuerID(client.RemoteSessionIssuerID.String()),
	)

	// Most upstreams advertise no revocation_endpoint, so this is the common
	// path and must not log.
	endpoint := ""
	if client.RevocationEndpoint.Valid {
		endpoint = client.RevocationEndpoint.String
	}
	if endpoint == "" {
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeSkipped
	}

	// The endpoint is a URL a customer's identity provider handed us, so it is
	// attacker-influenceable in the same way the token endpoint is. The guardian
	// policy below is the actual SSRF control; this check rejects values that
	// are not absolute HTTPS URLs. Tokens are sensitive credentials that must
	// not be transmitted in plaintext, so only https:// is accepted.
	if !urls.IsAbsoluteHTTPSOrLoopback(endpoint) {
		logger.WarnContext(ctx, "upstream revoke: issuer advertises an unusable revocation endpoint",
			attr.SlogOAuthFailureReason("revocation_endpoint must be an absolute https url, or http on loopback"),
		)
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeInternal
	}

	token, hint, ok := r.tokenToRevoke(ctx, cred)
	if !ok {
		// Either the session stored no token at all, or what it stored could not
		// be decrypted. Neither is recoverable and neither is the upstream's
		// fault; tokenToRevoke has already logged a decryption failure.
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeSkipped
	}

	var clientSecret string
	if client.ClientSecretEncrypted.Valid {
		clientSecret, err = r.enc.Decrypt(client.ClientSecretEncrypted.String)
		if err != nil {
			logger.WarnContext(ctx, "upstream revoke: client secret could not be read", attr.SlogError(err))
			return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeInternal
		}
	}

	// RFC 7009 §2.1: the revocation endpoint authenticates the client the same
	// way the token endpoint does, so this reuses the token path's resolution
	// and request builder rather than duplicating the credential-placement rules
	// (and their upstream-specific fixes) for a second endpoint.
	authMethod, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, clientSecret)
	if err != nil {
		logger.WarnContext(ctx, "upstream revoke: client auth configuration is invalid", attr.SlogError(err))
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeInternal
	}

	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", hint)

	req, err := newTokenEndpointRequest(ctx, endpoint, form, authMethod, client.ExternalClientID, clientSecret)
	if err != nil {
		logger.WarnContext(ctx, "upstream revoke: could not build request", attr.SlogError(err))
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeInternal
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.WarnContext(ctx, "upstream revoke: identity provider unreachable",
			attr.SlogOAuthGrant(hint),
			attr.SlogError(err),
		)
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeUnreachable
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Drained and discarded. RFC 7009 §2.2 specifies an empty body on success,
	// and an error body has no field Gram acts on — but the read still has to
	// happen for the connection to return to the pool.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode/100 != 2 {
		// Not escalated beyond a warning even for a 5xx. RFC 7009 §2.2.1 suggests
		// a client SHOULD retry a 503, but there is no durable retry here by
		// design (see the file comment), and paging on another operator's
		// authorization server having a bad minute is not actionable for Gram.
		logger.WarnContext(ctx, "upstream revoke: identity provider rejected the revocation",
			attr.SlogOAuthGrant(hint),
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
		)
		return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeRejected
	}

	// Info rather than Debug: "Gram asked a provider to destroy a token" is a
	// security-relevant event, and it is the only durable record that a
	// revocation reached the upstream. Every other outcome is either a warning
	// or a metric, so a silent success would leave the path with no evidence at
	// all when traces are unavailable.
	logger.InfoContext(ctx, "upstream revoke: token revoked",
		attr.SlogOAuthGrant(hint),
		attr.SlogOAuthIssuer(client.IssuerUrl),
	)
	return client.IssuerUrl, remotesessionmetrics.RevokeOutcomeSuccess
}

// tokenToRevoke picks which of the session's two credentials to send, returning
// the plaintext token and its RFC 7009 token_type_hint.
//
// The refresh token wins whenever one is stored. RFC 7009 §2.1 says revoking a
// refresh token SHOULD also invalidate the access tokens derived from it, while
// revoking an access token carries no such implication — so the refresh token is
// the one credential whose revocation can plausibly kill the whole grant, and
// the access token alone would leave the refresh token live to mint replacements.
//
// Only one request is sent. Following the refresh-token revoke with a second
// call for the access token would cover upstreams that ignore the SHOULD, at the
// cost of doubling the fan-out on every bulk revoke; the spec-conforming single
// call is the deliberate choice here.
func (r *UpstreamRevoker) tokenToRevoke(ctx context.Context, cred RevokedCredentials) (token string, hint string, ok bool) {
	encrypted, hint := cred.AccessTokenEncrypted, "access_token"
	if cred.RefreshTokenEncrypted.Valid && cred.RefreshTokenEncrypted.String != "" {
		encrypted, hint = cred.RefreshTokenEncrypted.String, "refresh_token"
	}
	if encrypted == "" {
		return "", "", false
	}

	plain, err := r.enc.Decrypt(encrypted)
	if err != nil {
		// Logged without the session id's token material and without retry: an
		// undecryptable token cannot be sent to anyone, so there is nothing to
		// recover. The local revoke has already taken effect regardless.
		r.logger.WarnContext(ctx, "upstream revoke: stored token could not be read",
			attr.SlogOAuthGrant(hint),
			attr.SlogError(fmt.Errorf("decrypt %s: %w", hint, err)),
		)
		return "", "", false
	}
	return plain, hint, true
}
