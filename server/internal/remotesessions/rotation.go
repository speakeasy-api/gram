package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// tunnelBindingID renders an issuer's tunnel binding for ProxyRegisterRequest,
// which carries it as an optional string. Empty means direct egress.
func tunnelBindingID(binding uuid.NullUUID) string {
	if !binding.Valid {
		return ""
	}
	return binding.UUID.String()
}

// RotationTrigger names why a client registration was replaced. It is carried
// on the log line and the audit entry so an operator can tell a rotation the
// issuer forced from one an administrator asked for.
type RotationTrigger string

const (
	// RotationTriggerUpstreamRejected: the issuer's token endpoint answered
	// invalid_client for the stored client_id, and a probe confirmed it.
	RotationTriggerUpstreamRejected RotationTrigger = "upstream_rejected"

	// RotationTriggerSecretExpired: the client_secret_expires_at the issuer
	// reported at registration has passed.
	RotationTriggerSecretExpired RotationTrigger = "secret_expired"

	// RotationTriggerManual: an administrator asked for the rotation.
	RotationTriggerManual RotationTrigger = "manual"
)

var (
	// ErrClientRotationInProgress reports that another caller holds the
	// rotation lease for the client; the row will carry the replacement once
	// that caller finishes.
	ErrClientRotationInProgress = errors.New("remotesessions: client registration rotation already in progress")

	// ErrIssuerHasNoRegistrationEndpoint reports that the client's issuer
	// publishes no RFC 7591 registration endpoint, so Gram has nowhere to
	// re-register the client.
	ErrIssuerHasNoRegistrationEndpoint = errors.New("remotesessions: issuer publishes no registration endpoint to re-register at")

	// ErrClientNotRotatable reports a client whose registration is not the
	// kind a dynamic registration produces: a CIMD-mode client, whose client_id
	// is the metadata document URL, or a private_key_jwt client, whose
	// registration is bound to a key set.
	ErrClientNotRotatable = errors.New("remotesessions: client registration cannot be replaced by dynamic registration")

	// ErrClientStillRecognized reports that the issuer's token endpoint still
	// authenticates the stored client, so the rejection that prompted the
	// rotation did not mean the registration was lost.
	ErrClientStillRecognized = errors.New("remotesessions: issuer still recognizes the client")
)

// clientRotationLeaseTTL bounds the single-flight lease around one
// re-registration. It covers the token endpoint probe, the registration POST
// (each with its own shorter timeout), and the persisting transaction, so a
// slow issuer cannot let the lease lapse while a second caller registers
// another replacement.
const clientRotationLeaseTTL = 60 * time.Second

// registrationProbeTimeout bounds the token endpoint probe that confirms an
// upstream rejection before the registration is replaced.
const registrationProbeTimeout = 10 * time.Second

// registrationProbeMaxBodyBytes caps how much of the probe response is read
// to classify it.
const registrationProbeMaxBodyBytes int64 = 64 * 1024

func clientRotationLeaseKey(clientID uuid.UUID) string {
	return "remotesession:rotate_registration:" + clientID.String()
}

// RotateClientRegistrationParams describes one rotation request.
type RotateClientRegistrationParams struct {
	// ClientID is the remote_session_clients row to re-register.
	ClientID uuid.UUID

	// Trigger names why the rotation runs.
	Trigger RotationTrigger

	// ExpectedClientID is the upstream client_id the caller decided to
	// replace. Automatic triggers set it: a login that decided on a snapshot
	// and then waited for the lease may find the row already repaired by the
	// caller that held it, and must adopt that repair rather than replace the
	// replacement or send its user out with the snapshot. Empty skips the
	// check; manual rotations always run.
	ExpectedClientID string

	// Actor is who the audit feed credits; automatic rotations pass the system
	// principal.
	Actor urn.Principal

	// ActorDisplayName labels the actor in the audit feed.
	ActorDisplayName *string

	// ConfirmUpstreamRejection probes the issuer's token endpoint with the
	// stored credentials before replacing anything, and aborts with
	// ErrClientStillRecognized when the issuer still authenticates the client.
	// The automatic path sets it so a single spurious invalid_client cannot
	// force every user of the client to reconnect.
	ConfirmUpstreamRejection bool

	// OrganizationID is the organization whose audit feed records the
	// rotation: the administrator's active organization, or the organization
	// the login belongs to. It covers legacy rows whose own organization_id was
	// never backfilled; an empty value falls back to the row's.
	OrganizationID string
}

// ClientRotator re-registers a remote_session_client with its issuer in
// place. The row keeps its id, so the user session issuer bindings, MCP server
// attachments, and key set links that reference it survive; only the upstream
// client_id, secret, and their issuer-reported stamps change. Every remote
// session minted against the old client_id is revoked, because the issuer
// binds grants to the client that obtained them and the replacement cannot
// redeem them.
type ClientRotator struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	enc    *encryption.Client
	policy *guardian.Policy

	// tunnels carries the probe and the re-registration for an issuer bound to
	// a tunneled MCP server. Both endpoints they reach — the token endpoint and
	// the registration endpoint — are private addresses for such an issuer, so
	// without this a rotation can never complete and the stale registration
	// stands: every login then fails at the issuer until an administrator
	// re-registers the client by hand.
	tunnels *tunnelrouting.HTTPClient

	serverURL   *url.URL
	locks       cache.Cache
	revoker     *UpstreamRevoker
	auditLogger *audit.Logger
}

// NewClientRotator wires a rotator over the same database, encryption key,
// egress policy, and lease cache the refresh path uses.
func NewClientRotator(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, policy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, locks cache.Cache, serverURL *url.URL, revoker *UpstreamRevoker, auditLogger *audit.Logger) *ClientRotator {
	return &ClientRotator{
		logger:      logger,
		db:          db,
		enc:         enc,
		policy:      policy,
		tunnels:     tunnels,
		serverURL:   serverURL,
		locks:       locks,
		revoker:     revoker,
		auditLogger: auditLogger,
	}
}

// Rotate replaces the client's upstream registration and returns the updated
// row. The replacement is registered at the registration endpoint the client's
// issuer publishes, as discovery last refreshed it, so the endpoint stays
// current across issuer metadata changes. A row another rotation moved first
// is returned as-is: the replacement it carries is the one to use, and the
// registration this call obtained is abandoned at the issuer.
//
// The lease, the probe, and the registration POST all happen outside a
// database transaction; the persisting write compares the client_id read at
// the start so a concurrent rotation is detected rather than blocked behind a
// row lock held across two outbound calls.
func (r *ClientRotator) Rotate(ctx context.Context, params RotateClientRegistrationParams) (repo.RemoteSessionClient, error) {
	var zero repo.RemoteSessionClient

	leaseKey := clientRotationLeaseKey(params.ClientID)
	leaseAcquiredAt := time.Now()
	held, err := r.locks.Add(ctx, leaseKey, clientRotationLeaseTTL)
	if err != nil {
		return zero, fmt.Errorf("acquire client rotation lease: %w", err)
	}
	if !held {
		return zero, ErrClientRotationInProgress
	}
	defer o11y.LogDefer(ctx, r.logger, "failed to release client rotation lease", func() error {
		// Past the TTL the key may already be a new holder's lease; deleting it
		// would let a third caller in beside them. Leave it to expire.
		if time.Since(leaseAcquiredAt) >= clientRotationLeaseTTL {
			r.logger.WarnContext(ctx, "client rotation outlived its lease; leaving release to TTL",
				attr.SlogRemoteSessionClientID(params.ClientID.String()),
				attr.SlogCacheKey(leaseKey),
			)
			return nil
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshReleaseTimeout)
		defer cancel()
		return r.locks.Delete(releaseCtx, leaseKey)
	})

	q := repo.New(r.db)
	row, err := q.GetRemoteSessionClientForRotation(ctx, params.ClientID)
	if err != nil {
		return zero, fmt.Errorf("load remote session client for rotation: %w", err)
	}
	current := row.RemoteSessionClient

	logger := r.logger.With(
		attr.SlogRemoteSessionClientID(current.ID.String()),
		attr.SlogOAuthIssuer(row.IssuerUrl),
		attr.SlogReason(string(params.Trigger)),
	)

	// The caller decided to rotate on a snapshot taken before it held the
	// lease. If the row moved in between, another rotation already replaced
	// the registration, or a probe already found the issuer still recognizes
	// it; either way the current row is the one to use and the issuer is not
	// contacted again. Rotating the replacement would revoke every session
	// its users just re-established, and probing the replacement only to hand
	// back the caller's snapshot would send its user out with the dead
	// client_id. This runs before every refusal below: a row another caller
	// already repaired is the one to use even when Gram could not rotate it
	// again, such as an issuer whose metadata has since lost its registration
	// endpoint.
	if params.Trigger != RotationTriggerManual {
		if params.ExpectedClientID != "" && current.ClientID != params.ExpectedClientID {
			logger.InfoContext(ctx, "client registration was already replaced by a concurrent rotation; adopting the replacement")
			return current, nil
		}
		if !rotationTriggerApplies(current, params.Trigger, time.Now()) {
			logger.InfoContext(ctx, "client registration no longer needs rotation; adopting the current row")
			return current, nil
		}
	}

	if current.ClientIDMetadataUri.Valid || TokenEndpointAuthMethod(current.TokenEndpointAuthMethod.String) == TokenEndpointAuthMethodPrivateKeyJWT {
		return zero, ErrClientNotRotatable
	}
	endpoint := strings.TrimSpace(row.IssuerRegistrationEndpoint.String)
	if endpoint == "" {
		return zero, ErrIssuerHasNoRegistrationEndpoint
	}

	if params.ConfirmUpstreamRejection {
		recognized, err := r.upstreamRecognizesClient(ctx, row)
		if err != nil {
			return zero, fmt.Errorf("probe issuer for client registration: %w", err)
		}
		if recognized {
			if _, err := q.ClearRemoteSessionClientUpstreamRejected(ctx, repo.ClearRemoteSessionClientUpstreamRejectedParams{ID: current.ID, ClientID: current.ClientID}); err != nil {
				logger.WarnContext(ctx, "failed to clear the upstream rejection marker on a client the issuer still recognizes", attr.SlogError(err))
			}
			logger.WarnContext(ctx, "issuer still recognizes the client registration; rotation skipped and rejection marker cleared")
			return zero, ErrClientStillRecognized
		}
	}

	// The replacement is registered over whatever transport the issuer names,
	// so a bound issuer's private registration endpoint is reachable. This is
	// the rotator's own system path: it re-registers a client an administrator
	// already authorized, at the endpoint the issuer row already carries, so
	// it does not widen who can create a tunnel binding.
	registered, err := RegisterDynamicClient(ctx, r.policy, r.tunnels, r.serverURL, ProxyRegisterRequest{
		RegistrationEndpoint:    endpoint,
		Scope:                   conv.PtrEmpty(strings.Join(current.Scope, " ")),
		TokenEndpointAuthMethod: conv.PtrEmpty(current.TokenEndpointAuthMethod.String),
		TunneledMcpServerID:     conv.PtrEmpty(tunnelBindingID(row.IssuerTunneledMcpServerID)),
	})
	if err != nil {
		return zero, fmt.Errorf("re-register client with issuer: %w", err)
	}

	var secretCiphertext pgtype.Text
	if registered.ClientSecret != "" {
		ciphertext, err := r.enc.Encrypt([]byte(registered.ClientSecret))
		if err != nil {
			return zero, fmt.Errorf("encrypt re-registered client secret: %w", err)
		}
		secretCiphertext = conv.ToPGText(ciphertext)
	}
	issuedAt := registered.ClientIDIssuedAt
	if !issuedAt.Valid {
		issuedAt = conv.ToPGTimestamptz(time.Now().UTC())
	}
	// An expiry that is not after the issuance would mark the replacement
	// expired the moment it lands, and every later login would rotate again
	// and revoke every session each time. Record no expiry instead; a
	// registration the issuer really drops still surfaces as invalid_client.
	secretExpiresAt := registered.ClientSecretExpiresAt
	if secretExpiresAt.Valid && !secretExpiresAt.Time.After(issuedAt.Time) {
		logger.WarnContext(ctx, "issuer reported a client secret expiry at or before its issuance; recording no expiry")
		secretExpiresAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}

	dbtx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin client rotation transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txRepo := repo.New(dbtx)

	issuerIDs, err := txRepo.ListUserSessionIssuerIDsForRemoteSessionClient(ctx, current.ID)
	if err != nil {
		return zero, fmt.Errorf("list client issuer bindings for rotation audit: %w", err)
	}
	beforeView, err := mv.BuildRemoteSessionClientView(current, issuerIDs)
	if err != nil {
		return zero, fmt.Errorf("build client view before rotation: %w", err)
	}

	updated, err := txRepo.ReplaceRemoteSessionClientRegistration(ctx, repo.ReplaceRemoteSessionClientRegistrationParams{
		ClientID:                registered.ClientID,
		ClientSecretEncrypted:   secretCiphertext,
		ClientIDIssuedAt:        issuedAt,
		ClientSecretExpiresAt:   secretExpiresAt,
		TokenEndpointAuthMethod: conv.ToPGTextEmpty(conv.Default(registered.TokenEndpointAuthMethod, current.TokenEndpointAuthMethod.String)),
		ID:                      current.ID,
		ExpectedClientID:        current.ClientID,
		ExpectedUpdatedAt:       current.UpdatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The row moved under us: another rotation, an administrator's update,
		// or a deletion. Report whatever it carries now and abandon the
		// registration this call obtained.
		moved, readErr := q.GetRemoteSessionClientForRotation(ctx, current.ID)
		if readErr != nil {
			return zero, fmt.Errorf("re-read remote session client after lost rotation race: %w", readErr)
		}
		logger.WarnContext(ctx, "client registration changed during rotation; keeping the concurrent replacement")
		return moved.RemoteSessionClient, nil
	}
	if err != nil {
		return zero, fmt.Errorf("persist re-registered client: %w", err)
	}

	cascaded, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, updated.ID)
	if err != nil {
		return zero, fmt.Errorf("revoke remote sessions of the replaced client: %w", err)
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated, issuerIDs)
	if err != nil {
		return zero, fmt.Errorf("build client view after rotation: %w", err)
	}
	// Global clients have no organization to credit the entry to; every other
	// tier records the rotation the same way an administrator's update is.
	auditOrganizationID := conv.Default(params.OrganizationID, updated.OrganizationID.String)
	if auditOrganizationID != "" {
		if err := r.auditLogger.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
			OrganizationID:         auditOrganizationID,
			ProjectID:              orgProjectID(updated.ProjectID),
			Actor:                  params.Actor,
			ActorDisplayName:       params.ActorDisplayName,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
			ClientID:               updated.ClientID,
			SnapshotBefore:         beforeView,
			SnapshotAfter:          afterView,
		}); err != nil {
			return zero, fmt.Errorf("log client registration rotation: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit client rotation: %w", err)
	}

	// The revoked sessions' tokens are dropped at the issuer on the same
	// best-effort terms as a delete: post-commit, bounded, never surfaced.
	r.revoker.RevokeAllDetached(ctx, revokedCredentials(cascaded))

	logger.InfoContext(ctx, "replaced the client registration with the issuer; remote sessions minted against the old client were revoked",
		attr.SlogDBUpdatedRowsCount(int64(len(cascaded))),
	)
	return updated, nil
}

// rotationTriggerApplies reports whether the condition that prompted a
// rotation still holds on the row as it is now. A rejection is outstanding
// while its marker is set; an expiry is outstanding while the recorded
// client_secret_expires_at is in the past. A replaced registration clears the
// marker and carries the issuer's new expiry, so either trigger stops applying
// once another caller has repaired the row.
func rotationTriggerApplies(row repo.RemoteSessionClient, trigger RotationTrigger, now time.Time) bool {
	switch trigger {
	case RotationTriggerUpstreamRejected:
		return row.UpstreamRejectedAt.Valid
	case RotationTriggerSecretExpired:
		return row.ClientSecretExpiresAt.Valid && !row.ClientSecretExpiresAt.Time.After(now)
	case RotationTriggerManual:
		return true
	default:
		return true
	}
}

// awaitRotation polls the client row while another caller holds the rotation
// lease, returning the row once its client_id no longer matches staleClientID
// or once the trigger the caller acted on no longer applies to it: the
// winner's probe cleared a rejection marker, or the winner's replacement
// carries an expiry that has not passed. An expiry-triggered wait starts with
// no rejection marker, so it must not treat the marker's absence as
// completion. It gives up after the refresh wait budget, the same bound the
// refresh path puts on waiting for a concurrent winner, and reports
// ErrClientRotationInProgress so the caller proceeds with what it has.
func (r *ClientRotator) awaitRotation(ctx context.Context, clientID uuid.UUID, staleClientID string, trigger RotationTrigger) (repo.RemoteSessionClient, error) {
	var zero repo.RemoteSessionClient
	q := repo.New(r.db)
	deadline := time.Now().Add(refreshWaitBudget)
	for {
		row, err := q.GetRemoteSessionClientForRotation(ctx, clientID)
		if err != nil {
			return zero, fmt.Errorf("poll remote session client during rotation: %w", err)
		}
		current := row.RemoteSessionClient
		if current.ClientID != staleClientID || !rotationTriggerApplies(current, trigger, time.Now()) {
			return current, nil
		}
		if time.Now().After(deadline) {
			return zero, ErrClientRotationInProgress
		}
		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("wait for concurrent client rotation: %w", ctx.Err())
		case <-time.After(refreshWaitPoll):
		}
	}
}

// upstreamRecognizesClient asks the issuer's token endpoint whether it still
// authenticates the stored client. A refresh_token grant with a made-up token
// is enough: an issuer authenticates the client before it looks at the grant,
// so invalid_client means the registration is gone, and an error the issuer
// can only produce after authenticating the client (invalid_grant above all)
// means the client is still on file. Anything else is an error rather than
// either verdict: a 5xx, a 429, a transport failure, an answer that cannot be
// classified, or an OAuth error such as temporarily_unavailable or
// server_error that says nothing about the client. Treating those as
// recognition would clear a genuine rejection marker while the grant is
// already gone, leaving the client reconnecting with the dead client_id until
// its next rejection; so an issuer having a bad minute neither rotates nor
// exonerates the client.
func (r *ClientRotator) upstreamRecognizesClient(ctx context.Context, row repo.GetRemoteSessionClientForRotationRow) (bool, error) {
	client := row.RemoteSessionClient
	tokenEndpoint := row.IssuerTokenEndpoint.String
	if tokenEndpoint == "" {
		return false, fmt.Errorf("issuer %q has no token endpoint to probe", row.IssuerUrl)
	}

	clientSecret := ""
	if client.ClientSecretEncrypted.Valid {
		secret, err := r.enc.Decrypt(client.ClientSecretEncrypted.String)
		if err != nil {
			return false, fmt.Errorf("decrypt stored client secret: %w", err)
		}
		clientSecret = secret
	}
	method, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, clientSecret)
	if err != nil {
		return false, fmt.Errorf("resolve token endpoint auth method: %w", err)
	}

	probeToken, err := randomToken(16)
	if err != nil {
		return false, fmt.Errorf("generate probe refresh token: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", probeToken)

	probeCtx, cancel := context.WithTimeout(ctx, registrationProbeTimeout)
	defer cancel()
	req, err := newTokenEndpointRequest(probeCtx, tokenEndpoint, form, tokenEndpointClientAuth{
		Method:                method,
		RemoteSessionClientID: client.ID,
		OrganizationID:        client.OrganizationID.String,
		JSONWebKeySetID:       client.JsonWebKeySetID.UUID,
		ClientID:              client.ClientID,
		ClientSecret:          clientSecret,
		AssertionAudience:     "",
		AssertionSigner:       nil,
	})
	if err != nil {
		return false, fmt.Errorf("build probe request: %w", err)
	}
	doer, err := upstreamHTTPDoer(r.policy.Client(), r.tunnels, row.IssuerTunneledMcpServerID)
	if err != nil {
		return false, fmt.Errorf("select probe transport: %w", err)
	}
	resp, err := doer.Do(req)
	if err != nil {
		return false, fmt.Errorf("post probe to token endpoint: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, registrationProbeMaxBodyBytes))
	if err != nil {
		return false, fmt.Errorf("read probe response: %w", err)
	}
	if resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests {
		return false, fmt.Errorf("token endpoint answered the probe with HTTP %d", resp.StatusCode)
	}
	parsed, ok := oautherr.ParseTokenError(body)
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			// A token for a made-up grant is not something to explain away;
			// whatever the issuer did, it authenticated the client to do it.
			return true, nil
		}
		return false, fmt.Errorf("token endpoint answered the probe with HTTP %d and no recognizable OAuth error", resp.StatusCode)
	}
	switch code := oautherr.CanonicalTokenErrorCode(parsed.Code); code {
	case oautherr.CodeInvalidClient:
		return false, nil
	case oautherr.CodeInvalidGrant, oautherr.CodeUnauthorizedClient, oautherr.CodeInvalidScope, oautherr.CodeUnsupportedGrantType:
		// Each of these is a verdict on the grant or the client's permissions,
		// which the issuer only reaches after authenticating the client.
		return true, nil
	default:
		return false, fmt.Errorf("token endpoint answered the probe with %q, which does not say whether the client is registered", code)
	}
}

// rotateRejectedRegistration replaces the client's upstream registration when
// the issuer has stopped recognizing it, returning the client to send to the
// authorize endpoint. Any failure leaves the client as it was: the login then
// fails at the issuer exactly as it would have without this step, and the
// warning here names why.
func (m *ChallengeManager) rotateRejectedRegistration(ctx context.Context, organizationID string, client Client) Client {
	trigger, needed := client.needsRegistrationRotation(time.Now())
	if !needed {
		return client
	}

	actorLabel := "System"
	rotated, err := m.rotator.Rotate(ctx, RotateClientRegistrationParams{
		ClientID:                 client.ID,
		Trigger:                  trigger,
		ExpectedClientID:         client.ExternalClientID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName:         &actorLabel,
		ConfirmUpstreamRejection: trigger == RotationTriggerUpstreamRejected,
		OrganizationID:           organizationID,
	})
	if errors.Is(err, ErrClientStillRecognized) {
		// The rejection was spurious and its marker is already cleared; the
		// stored client is the right one to send.
		return client
	}
	if errors.Is(err, ErrClientRotationInProgress) {
		// Another login is replacing the registration right now. Sending this
		// user to the authorize endpoint with the client it is replacing would
		// land them on the issuer's error page, so wait for the winner's row
		// instead; it finishes within a few seconds.
		rotated, err = m.rotator.awaitRotation(ctx, client.ID, client.ExternalClientID, trigger)
	}
	if err != nil {
		m.logger.WarnContext(ctx, "could not replace a client registration the issuer no longer recognizes; proceeding with the stored client",
			attr.SlogRemoteSessionClientID(client.ID.String()),
			attr.SlogOAuthIssuer(client.IssuerURL),
			attr.SlogReason(string(trigger)),
			attr.SlogError(err),
		)
		return client
	}

	client.ExternalClientID = rotated.ClientID
	client.ClientSecretEncrypted = conv.FromPGText[string](rotated.ClientSecretEncrypted)
	client.LegacyCallbackUrl = rotated.LegacyCallbackUrl
	client.ClientSecretExpiresAt = timestampPtr(rotated.ClientSecretExpiresAt)
	client.UpstreamRejectedAt = timestampPtr(rotated.UpstreamRejectedAt)
	return client
}

// timestampPtr is the nullable form of a pgtype.Timestamptz.
func timestampPtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
