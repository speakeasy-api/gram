package clientcredentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	// expirySkew is how long before its upstream expiry a credential stops
	// being served, so a proxied call never starts with a token that expires
	// in flight. Short-lived tokens use half their lifetime instead.
	expirySkew = time.Minute

	// unknownExpiryLifetime caps reuse of a token whose provider reported no
	// expiry. Short enough that a revoked token is replaced soon; long enough
	// that a busy client does not request a token per call.
	unknownExpiryLifetime = 5 * time.Minute

	// maxCredentialLifetime caps reuse of any token, so a token the upstream
	// revoked is replaced within the hour even if no request reports it.
	maxCredentialLifetime = time.Hour
)

// grant runs the client credentials grant for client, sending resource unless
// it is empty, and stores the result under keys. A credential that cannot be
// stored is still returned.
func (m *Minter) grant(ctx context.Context, logger *slog.Logger, client repo.GetClientCredentialsGrantClientRow, resource string, keys cacheKeys) (Credential, error) {
	var none Credential

	endpoint, err := m.endpoints.BindTokenEndpoint(remotesessions.TokenEndpointRegistration{
		ClientID:                        client.ClientID,
		OrganizationID:                  client.OrganizationID.String,
		ExternalClientID:                client.ExternalClientID,
		ClientSecretEncrypted:           client.ClientSecretEncrypted.String,
		TokenEndpointAuthMethod:         client.TokenEndpointAuthMethod.String,
		TokenEndpointAuthAudienceFormat: client.TokenEndpointAuthAudienceFormat.String,
		JSONWebKeySetID:                 client.JsonWebKeySetID,
		IssuerID:                        client.IssuerID,
		IssuerURL:                       client.IssuerUrl,
		IssuerMetadata:                  client.IssuerMetadata,
		TokenEndpoint:                   client.TokenEndpoint.String,
		TunneledMcpServerID:             client.TunneledMcpServerID,
	})
	if err != nil {
		return none, fmt.Errorf("bind client credentials token endpoint: %w", err)
	}

	now := m.now()

	switch endpoint.AuthMethod() {
	case remotesessions.TokenEndpointAuthMethodNone:
		// RFC 6749 §4.4: only a confidential client may use this grant.
		return none, configurationError("client credentials grant requires an authenticated client")
	case remotesessions.TokenEndpointAuthMethodBasic, remotesessions.TokenEndpointAuthMethodPost:
		if client.ClientSecretExpiresAt.Valid && !now.Before(client.ClientSecretExpiresAt.Time) {
			return none, configurationError("client secret has expired")
		}
	case remotesessions.TokenEndpointAuthMethodPrivateKeyJWT:
	}

	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeClientCredentials)

	if len(client.ClientScope) > 0 {
		form.Set(oauthwire.ParamScope, strings.Join(client.ClientScope, " "))
	}

	if client.ClientAudience.String != "" {
		form.Set(oauthwire.ParamAudience, client.ClientAudience.String)
	}

	if resource != "" {
		form.Set(oauthwire.ParamResource, resource)
	}

	postCtx, cancel := context.WithTimeout(ctx, grantTimeout)
	defer cancel()

	tok, err := endpoint.Post(postCtx, form)
	if rejected, ok := errors.AsType[*remotesessions.TokenEndpointError](err); ok && resource != "" && rejected.Code == oautherr.CodeInvalidTarget {
		// RFC 8707 invalid_target rejects this resource; the grant itself may
		// still succeed without it.
		logger.DebugContext(ctx, "token endpoint rejected the RFC 8707 resource parameter; retrying without it",
			attr.SlogOAuthIssuer(client.IssuerUrl),
			attr.SlogOAuthResource(resource),
		)
		form.Del(oauthwire.ParamResource)

		tok, err = endpoint.Post(postCtx, form)
	}

	if err != nil {
		return none, fmt.Errorf("client credentials grant: %w", err)
	}

	// RFC 6749 §7.1: a client must not use a token whose type it does not
	// understand. A missing token_type is presented as a bearer token.
	if tokenType := tok.TokenType(); tokenType != "" && !strings.EqualFold(tokenType, string(SchemeBearer)) {
		return none, configurationError("token endpoint issued a token type other than Bearer")
	}

	expiresAt := servedUntil(now, tok.AccessExpiresAt(now))
	// The request may consume the token's entire usable lifetime. Keep the
	// conservative request-time deadline, but validate it at receipt time.
	receivedAt := m.now()
	if !expiresAt.After(receivedAt) {
		return none, errors.New("token endpoint issued an access token that has already expired")
	}

	encrypted, err := m.enc.Encrypt([]byte(tok.AccessToken()))
	if err != nil {
		return none, fmt.Errorf("encrypt client credential: %w", err)
	}

	entry := credentialEntry{
		Key:                  keys.credential,
		AccessTokenEncrypted: encrypted,
		Scheme:               SchemeBearer,
		ExpiresAt:            expiresAt,
		MintedAt:             now,
		ttl:                  expiresAt.Sub(receivedAt),
	}
	cred := Credential{
		value:     tok.AccessToken(),
		scheme:    SchemeBearer,
		expiresAt: expiresAt,
		mintedAt:  now,
		entry:     &entry,
	}

	storeCtx, storeCancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer storeCancel()

	if err := m.credentials.Store(storeCtx, entry); err != nil {
		logger.WarnContext(ctx, "store client credential", attr.SlogCacheKey(keys.credential), attr.SlogError(err))

		cred.entry = nil
	}

	return cred, nil
}

// sentResource is the resource the grant sends for requested. Only an issuer
// known to reject resource indicators keeps it off the grant, matching the
// authorization and refresh grants; the credential is then the same for every
// requested resource.
func sentResource(client repo.GetClientCredentialsGrantClientRow, requested string) string {
	if client.ResourceIndicatorSupported.Valid && !client.ResourceIndicatorSupported.Bool {
		return ""
	}

	return requested
}

// servedUntil is when a token requested at now stops being served: its
// reported expiry, or unknownExpiryLifetime without one, capped at
// maxCredentialLifetime and less expirySkew or half its lifetime, whichever is
// shorter.
func servedUntil(now time.Time, reported *time.Time) time.Time {
	expiresAt := now.Add(unknownExpiryLifetime)
	if reported != nil {
		expiresAt = *reported
	}

	if capped := now.Add(maxCredentialLifetime); expiresAt.After(capped) {
		expiresAt = capped
	}

	lifetime := expiresAt.Sub(now)
	if lifetime <= 0 {
		return expiresAt
	}

	return expiresAt.Add(-min(expirySkew, lifetime/2))
}

// configurationError marks a registration or upstream behavior an
// administrator must fix.
func configurationError(reason string) error {
	return fmt.Errorf("%w: %s", remotesessions.ErrTokenEndpointConfiguration, reason)
}
