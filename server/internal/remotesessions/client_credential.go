package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// CredentialOwner is who the upstream credential a remote session client
// obtains belongs to, as stored in remote_session_clients.credential_owner.
type CredentialOwner string

const (
	// CredentialOwnerSubject marks a credential a session subject obtains by
	// connecting the client: each caller presents their own grant.
	CredentialOwnerSubject CredentialOwner = "subject"

	// CredentialOwnerSelf marks a credential the client holds for itself, such
	// as one from the client credentials grant: every caller presents the same
	// one and nobody connects the client.
	CredentialOwnerSelf CredentialOwner = "self"
)

// ErrClientCredentialClientNotFound reports that the organization has no live
// client with the requested id. Global clients are never found: their
// credential would be shared across tenants.
var ErrClientCredentialClientNotFound = errors.New("remotesessions: client credentials client not found in organization")

// ErrSelfCredentialClient reports a subject connection attempted against a
// client that holds its upstream credential for itself. Nobody connects such a
// client, so there is no authorize redirect to mint.
var ErrSelfCredentialClient = errors.New("remotesessions: client holds its own upstream credential and is not connected by subjects")

// ErrClientCredentialMisconfigured means the credential a self client holds
// for itself cannot be obtained, or the upstream rejected it, for a reason
// only an administrator can repair. It refines ErrRemoteSessionMisconfigured,
// so remedy-only callers treat it as one, but no authorization the caller
// performs supplies the credential: an MCP endpoint answers it with an error
// that names the administrator, never with a challenge.
var ErrClientCredentialMisconfigured = fmt.Errorf("%w: client credential misconfigured", ErrRemoteSessionMisconfigured)

// ErrClientCredentialUnavailable means the credential a self client holds for
// itself could not be obtained for a reason that clears on its own. It
// refines ErrRemoteSessionUnavailable, and the same request is expected to
// succeed later.
var ErrClientCredentialUnavailable = fmt.Errorf("%w: client credential temporarily unavailable", ErrRemoteSessionUnavailable)

// ClientCredentialSource obtains the upstream credential a remote session
// client holds for itself rather than for a session subject. The client
// credentials grant is one source; a stored credential, such as an API key,
// can be another.
type ClientCredentialSource interface {
	// Credential returns a usable upstream credential for the request. Errors
	// a caller can classify: ErrClientCredentialClientNotFound,
	// ErrTokenEndpointConfiguration, and *TokenEndpointError.
	Credential(ctx context.Context, req ClientCredentialRequest) (ClientCredential, error)
}

// ClientCredentialRequest names the client whose upstream credential is
// wanted.
type ClientCredentialRequest struct {
	// OrganizationID is the tenant the client must belong to.
	OrganizationID string

	// ClientID is the remote_session_clients row.
	ClientID uuid.UUID

	// Resource is the RFC 8707 resource indicator, sent verbatim. Empty sends
	// none.
	Resource string
}

// ClientCredentialScheme is how a client credential is presented to the
// upstream.
type ClientCredentialScheme string

const (
	// ClientCredentialSchemeBearer presents the credential as an RFC 6750
	// bearer token in the Authorization header.
	ClientCredentialSchemeBearer ClientCredentialScheme = "Bearer"
)

// ClientCredential is an upstream credential a client holds for itself. It
// formats redacted; only Value exposes the secret.
type ClientCredential struct {
	// value is the secret presented upstream.
	value string

	// scheme is how value is presented upstream.
	scheme ClientCredentialScheme

	// expiresAt is when the credential stops being served.
	expiresAt time.Time

	// forget discards the credential from its source; nil when the source
	// cannot replace it.
	forget func(context.Context) (bool, error)
}

// NewClientCredential builds a credential for a ClientCredentialSource.
// forget discards it after the upstream rejected it and reports whether the
// source can return a different one; nil means it cannot.
func NewClientCredential(value string, scheme ClientCredentialScheme, expiresAt time.Time, forget func(context.Context) (bool, error)) ClientCredential {
	return ClientCredential{value: value, scheme: scheme, expiresAt: expiresAt, forget: forget}
}

// Value is the secret presented to the upstream.
func (c ClientCredential) Value() string { return c.value }

// Scheme is how Value is presented to the upstream.
func (c ClientCredential) Scheme() ClientCredentialScheme { return c.scheme }

// ExpiresAt is when the credential stops being served, ahead of its upstream
// expiry so a proxied call never starts with a credential that expires in
// flight.
func (c ClientCredential) ExpiresAt() time.Time { return c.expiresAt }

// Forget discards the credential after the upstream rejected it, and reports
// whether a later Credential call can return a different one. False, with no
// error, means the rejection is final: the source cannot replace the
// credential, or replaced it moments ago and the upstream would reject the
// replacement too.
func (c ClientCredential) Forget(ctx context.Context) (bool, error) {
	if c.forget == nil {
		return false, nil
	}

	return c.forget(ctx)
}

func (c ClientCredential) String() string               { return "[redacted client credential]" }
func (c ClientCredential) GoString() string             { return c.String() }
func (c ClientCredential) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (c ClientCredential) LogValue() slog.Value         { return slog.StringValue(c.String()) }

// unconfiguredClientCredentialSource serves a manager built without a source,
// so a self client fails as misconfigured instead of resolving no token.
type unconfiguredClientCredentialSource struct{}

func (unconfiguredClientCredentialSource) Credential(context.Context, ClientCredentialRequest) (ClientCredential, error) {
	return ClientCredential{}, fmt.Errorf("%w: client credentials are not supported by this deployment", ErrTokenEndpointConfiguration)
}

// WithClientCredentialSource serves self clients from the source build
// returns. build receives the constructed manager, because a source obtains
// credentials through the manager's token endpoint bindings.
func WithClientCredentialSource(build func(*ChallengeManager) ClientCredentialSource) ChallengeManagerOption {
	return func(m *ChallengeManager) { m.clientCredentialBuilder = build }
}

// resolveClientCredential obtains the credential a self client holds for
// itself. The caller plays no part in selecting it: no subject owns the
// credential, so users, agents and workloads all present the same one, and
// there is no grant row to bind, recheck or touch.
//
// The token records the resource the grant requested, which the credential
// is audience-bound to, and routes by the remote_session_issuer it is keyed
// under: only to an upstream that is that resource, or anywhere the issuer
// serves when the grant requested none.
func (m *ChallengeManager) resolveClientCredential(ctx context.Context, req ClientCredentialRequest) (UpstreamToken, error) {
	var zero UpstreamToken

	cred, err := m.clientCredentials.Credential(ctx, req)
	if err == nil && cred.Scheme() != ClientCredentialSchemeBearer {
		err = fmt.Errorf("%w: client credential scheme %q is not a bearer token", ErrTokenEndpointConfiguration, cred.Scheme())
	}
	if err == nil && cred.Value() == "" {
		err = fmt.Errorf("%w: client credential is empty", ErrTokenEndpointConfiguration)
	}
	if err != nil {
		classified := classifyClientCredentialError(err)
		// The source logs the grant's own failure; callers log the remedy.
		m.logger.DebugContext(ctx, "remote session client credential unavailable",
			attr.SlogOrganizationID(req.OrganizationID),
			attr.SlogRemoteSessionClientID(req.ClientID.String()),
			attr.SlogError(err),
		)

		return zero, classified
	}

	return UpstreamToken{
		Token:                              cred.Value(),
		Resource:                           req.Resource,
		CredentialOwner:                    CredentialOwnerSelf,
		RemoteSessionClientID:              req.ClientID,
		RemoteSessionID:                    uuid.Nil,
		RemoteSessionUpdatedAt:             time.Time{},
		RemoteSessionResolvedFromUpdatedAt: time.Time{},
		GrantGeneration:                    0,
		AccessExpiresAt:                    clientCredentialExpiry(cred),
		ClientCredentialErr:                nil,
		renewal:                            &clientCredentialRenewal{credential: cred, request: req},
	}, nil
}

// clientCredentialExpiry is Speakeasy's serving cutoff for cred: when it stops
// presenting the credential. Its source sets one even when the token endpoint
// stated no expiry, so unlike a subject grant's expiry it is present whenever
// the source reports one, and it can be earlier than the upstream's.
func clientCredentialExpiry(cred ClientCredential) *time.Time {
	if cred.ExpiresAt().IsZero() {
		return nil
	}
	return new(cred.ExpiresAt())
}

// RenewClientCredential replaces a self client's credential after the
// upstream rejected it, so the caller can retry once with the replacement. It
// reports false, with no error, when there is nothing to retry with: tok is
// not a self credential, or its source keeps it because it was obtained
// moments ago and the upstream would reject a replacement too. The caller then
// treats the rejection as final. An error is the replacement failing, already
// classified as ErrClientCredentialMisconfigured or
// ErrClientCredentialUnavailable.
func (m *ChallengeManager) RenewClientCredential(ctx context.Context, tok UpstreamToken) (UpstreamToken, bool, error) {
	var zero UpstreamToken

	if tok.CredentialOwner != CredentialOwnerSelf || tok.Token == "" || tok.renewal == nil {
		return zero, false, nil
	}

	forgotten, err := tok.renewal.credential.Forget(ctx)
	if err != nil {
		return zero, false, fmt.Errorf("%w: forget rejected client credential: %w", ErrClientCredentialUnavailable, err)
	}
	if !forgotten {
		return zero, false, nil
	}

	renewed, err := m.resolveClientCredential(ctx, tok.renewal.request)
	if err != nil {
		return zero, false, err
	}

	return renewed, true, nil
}

// classifyClientCredentialError names the remedy for a failed client
// credential. Only an administrator can repair a missing client, a
// registration Speakeasy cannot authenticate with, or a token endpoint that
// rejected the request; anything else (an unreachable or failing token
// endpoint, a rate limit, a signing or cache outage) clears on its own.
func classifyClientCredentialError(err error) error {
	if errors.Is(err, ErrClientCredentialClientNotFound) || errors.Is(err, ErrTokenEndpointConfiguration) {
		return fmt.Errorf("%w: %w", ErrClientCredentialMisconfigured, err)
	}

	if rejected, ok := errors.AsType[*TokenEndpointError](err); ok && clientCredentialRejection(rejected) {
		return fmt.Errorf("%w: %w", ErrClientCredentialMisconfigured, err)
	}

	return fmt.Errorf("%w: %w", ErrClientCredentialUnavailable, err)
}

// clientCredentialRejection reports a token endpoint answer that repeating the
// same request cannot change: a 4xx other than a timeout or rate limit.
func clientCredentialRejection(rejected *TokenEndpointError) bool {
	if rejected.Transport || rejected.Signing || rejected.StatusCode/100 != 4 {
		return false
	}

	return rejected.StatusCode != http.StatusRequestTimeout && rejected.StatusCode != http.StatusTooManyRequests
}

// clientCredentialRequestResource is the RFC 8707 resource a self client's
// grant sends: the resource identifier the client was registered for, or the
// upstream derived from the MCP servers it is attached to when it records
// none. derived is empty when those servers are absent or ambiguous, and the
// grant then sends no resource.
func clientCredentialRequestResource(c remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow, derived string) string {
	if c.ResourceIdentifier.Valid && c.ResourceIdentifier.String != "" {
		return c.ResourceIdentifier.String
	}

	return derived
}
