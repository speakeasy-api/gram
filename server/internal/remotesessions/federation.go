package remotesessions

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Federation errors deliberately omit upstream response bodies and credentials.
var (
	ErrFederatedConfiguration = errors.New("federated identity provider configuration is unavailable or invalid")
	ErrFederatedSigning       = errors.New("federated identity provider signing failed")
	ErrFederatedUnavailable   = errors.New("federated identity provider is temporarily unavailable")
	ErrFederatedIdentity      = errors.New("federated identity provider authentication failed")
)

// FederatedProvider is a request-local snapshot, never a decrypted-secret cache.
// Callers must reload it and compare Fingerprint before exchanging a callback.
type FederatedProvider struct {
	organizationID string
	client         repo.RemoteSessionClient
	issuer         repo.RemoteSessionIssuer
	metadata       rfc8414Document
	fingerprint    string
}

func (p *FederatedProvider) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

func (p *FederatedProvider) String() string      { return "[federated provider]" }
func (p *FederatedProvider) GoString() string    { return p.String() }
func (p *FederatedProvider) Fingerprint() string { return p.fingerprint }

// ValidateResponseIssuer implements RFC 9207 before the code leaves Gram.
// The response issuer is never used to select a provider.
func (p *FederatedProvider) ValidateResponseIssuer(issuer string) error {
	if p == nil || (issuer == "" && p.metadata.AuthorizationResponseIssParameterSupported) || (issuer != "" && issuer != p.issuer.Issuer) {
		return ErrFederatedIdentity
	}
	return nil
}

// LoadFederatedProvider resolves only a trusted organization-owned registration
// against the endpoint's organization. The IDs must come from the live USI.
func (m *ChallengeManager) LoadFederatedProvider(ctx context.Context, organizationID string, issuerID, clientID uuid.UUID) (*FederatedProvider, error) {
	if organizationID == "" || issuerID == uuid.Nil || clientID == uuid.Nil {
		return nil, ErrFederatedConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	row, err := repo.New(m.db).GetTrustedRemoteSessionClientForOrganization(ctx, repo.GetTrustedRemoteSessionClientForOrganizationParams{OrganizationID: organizationID, IssuerID: issuerID, ClientID: clientID})
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	issuerURL, err := url.Parse(row.RemoteSessionIssuer.Issuer)
	if err != nil || !validIssuerDiscoveryURL(issuerURL) || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, ErrFederatedConfiguration
	}
	doer, err := upstreamHTTPDoer(noRedirectClient(m.policy.PooledClient()), m.tunnels, row.RemoteSessionIssuer.TunneledMcpServerID)
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	// OIDC Discovery uses the issuer path, without OAuth metadata fallbacks or
	// the package's legacy trailing-slash issuer normalization.
	doc, discoveryErr := m.loadFederatedMetadata(ctx, organizationID, row.RemoteSessionIssuer, doer)
	if discoveryErr != nil {
		return nil, ErrFederatedConfiguration
	}
	p, err := newFederatedProvider(organizationID, row.RemoteSessionIssuer, row.RemoteSessionClient, doc)
	if err != nil {
		return nil, err
	}
	if err := m.preflightFederatedSigner(p); err != nil {
		return nil, err
	}
	return p, nil
}

func newFederatedProvider(organizationID string, issuer repo.RemoteSessionIssuer, client repo.RemoteSessionClient, doc rfc8414Document) (*FederatedProvider, error) {
	issuerURL, err := url.Parse(issuer.Issuer)
	if err != nil || !validIssuerDiscoveryURL(issuerURL) || doc.Issuer != issuer.Issuer || doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JwksURI == "" || client.ClientID == "" {
		return nil, ErrFederatedConfiguration
	}
	if err := validateIssuerMetadataEndpoints(doc, issuerURL); err != nil {
		return nil, ErrFederatedConfiguration
	}
	for _, endpoint := range []string{doc.AuthorizationEndpoint, doc.TokenEndpoint, doc.JwksURI} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Fragment != "" || u.User != nil {
			return nil, ErrFederatedConfiguration
		}
	}
	jwksURL, err := url.Parse(doc.JwksURI)
	if err != nil || jwksURL.Scheme != "https" {
		return nil, ErrFederatedConfiguration
	}
	if doc.ResponseTypesSupported != nil && !slices.Contains(doc.ResponseTypesSupported, "code") {
		return nil, ErrFederatedConfiguration
	}
	effectiveScopes := federatedScopes(issuer, client)
	if !slices.Contains(effectiveScopes, "openid") || !slices.Contains(effectiveScopes, "email") {
		return nil, ErrFederatedConfiguration
	}
	if len(doc.CodeChallengeMethodsSupported) > 0 && !slices.Contains(doc.CodeChallengeMethodsSupported, "S256") {
		return nil, ErrFederatedConfiguration
	}
	if _, err := acceptedIDTokenAlgorithms(doc.IDTokenSigningAlgValuesSupported); err != nil {
		return nil, ErrFederatedConfiguration
	}
	// Check authentication using secret presence only. Decrypt just at exchange.
	secret := ""
	if client.ClientSecretEncrypted.Valid && client.ClientSecretEncrypted.String != "" {
		secret = "present"
	}
	method, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, secret)
	if err != nil || method == TokenEndpointAuthMethodNone {
		return nil, ErrFederatedConfiguration
	}
	if method == TokenEndpointAuthMethodPrivateKeyJWT && !client.JsonWebKeySetID.Valid {
		return nil, ErrFederatedConfiguration
	}
	if len(doc.TokenEndpointAuthMethodsSupported) > 0 && !slices.Contains(doc.TokenEndpointAuthMethodsSupported, string(method)) {
		return nil, ErrFederatedConfiguration
	}
	if client.ClientSecretExpiresAt.Valid && !client.ClientSecretExpiresAt.Time.After(time.Now()) {
		return nil, ErrFederatedConfiguration
	}
	// Exclude JWKS cache contents/timestamps but bind secret rotations and live
	// registration, trust, endpoint and metadata policy changes.
	snapshot := struct {
		Organization  string                  `json:"Organization"`
		Client        federatedClientSnapshot `json:"Client"`
		IssuerID      uuid.UUID               `json:"IssuerID"`
		Issuer        string                  `json:"Issuer"`
		IssuerVersion string                  `json:"IssuerVersion"`
		Authorization string                  `json:"Authorization"`
		Token         string                  `json:"Token"`
		JWKS          string                  `json:"JWKS"`
		Tunnel        uuid.NullUUID           `json:"Tunnel"`
		Metadata      rfc8414Document         `json:"Metadata"`
	}{Organization: organizationID, Client: federatedClientSnapshot(client), IssuerID: issuer.ID, Issuer: issuer.Issuer, IssuerVersion: federatedIssuerVersion(issuer), Authorization: issuer.AuthorizationEndpoint.String, Token: issuer.TokenEndpoint.String, JWKS: issuer.JwksUri.String, Tunnel: issuer.TunneledMcpServerID, Metadata: doc}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	digest := sha256.Sum256(encoded)
	return &FederatedProvider{organizationID: organizationID, client: client, issuer: issuer, metadata: doc, fingerprint: hex.EncodeToString(digest[:])}, nil
}

func (p *FederatedProvider) BuildAuthorizationURL(callbackURL, state, nonce, verifier string) (*url.URL, error) {
	if p == nil || state == "" || nonce == "" || !validFederatedVerifier(verifier) {
		return nil, ErrFederatedIdentity
	}
	callback, err := url.Parse(callbackURL)
	if err != nil || !validIssuerDiscoveryURL(callback) || callback.Fragment != "" {
		return nil, ErrFederatedIdentity
	}
	u, err := url.Parse(p.metadata.AuthorizationEndpoint)
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	effectiveScopes := federatedScopes(p.issuer, p.client)
	scope := make([]string, 0, len(effectiveScopes))
	for _, item := range effectiveScopes {
		if item != "offline_access" {
			scope = append(scope, item)
		}
	}
	q := u.Query()
	// Preserve endpoint query parameters used for provider routing, but remove
	// parameters that could change login, consent, or requested access policy.
	// AICP sets the core OAuth/OIDC parameters below; endpoint metadata must not
	// supply an alternate request object or opt this flow into offline consent.
	removedParameters := []string{
		"prompt",
		"access_type",
		"resource",
		"audience",
		"request",
		"request_uri",
		"id_token_hint",
		"login_hint",
		"claims",
		"acr_values",
		"max_age",
		"authorization_details",
		"include_granted_scopes",
		"approval_prompt",
	}
	for _, parameter := range removedParameters {
		q.Del(parameter)
	}
	q.Set("response_type", "code")
	q.Set("response_mode", "query")
	q.Set("client_id", p.client.ClientID)
	q.Set("redirect_uri", callbackURL)
	q.Set("scope", strings.Join(scope, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	challenge := sha256.Sum256([]byte(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	u.RawQuery = q.Encode()
	return u, nil
}

func validFederatedVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, c := range verifier {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("-._~", c) {
			return false
		}
	}
	return true
}

// FederatedIdentity contains only verified identity claims. EmailVerified keeps
// absent distinct from false so shared provisioned-human policy can decide.
// There is intentionally no exported token accessor or credential JSON field.
type FederatedIdentity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified *bool
	credentials   *EphemeralFederatedCredentials
}

// ExchangeFederatedCode must run only after browser/state/issuer validation and
// live fingerprint comparison by the callback owner. It never persists tokens.
func (m *ChallengeManager) ExchangeFederatedCode(ctx context.Context, p *FederatedProvider, callbackURL, code, nonce, verifier string) (*FederatedIdentity, error) {
	if p == nil || code == "" || nonce == "" || !validFederatedVerifier(verifier) {
		return nil, ErrFederatedIdentity
	}
	if _, err := p.BuildAuthorizationURL(callbackURL, "validation", nonce, verifier); err != nil {
		return nil, err
	}
	if err := m.preflightFederatedSigner(p); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.validateFederatedMetadataHosts(ctx, p.issuer, p.metadata); err != nil {
		return nil, err
	}
	secret := ""
	if p.client.ClientSecretEncrypted.Valid && p.client.TokenEndpointAuthMethod.String != string(TokenEndpointAuthMethodPrivateKeyJWT) {
		var err error
		secret, err = m.enc.Decrypt(p.client.ClientSecretEncrypted.String)
		if err != nil {
			return nil, ErrFederatedConfiguration
		}
	}
	method, err := ResolveTokenEndpointAuthMethod(p.client.TokenEndpointAuthMethod.String, secret)
	if err != nil || method == TokenEndpointAuthMethodNone {
		return nil, ErrFederatedConfiguration
	}
	audience, err := ResolveTokenEndpointAuthAudience(p.client.TokenEndpointAuthAudienceFormat.String, p.issuer.Issuer, p.metadata.TokenEndpoint)
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	doer, err := upstreamHTTPDoer(noRedirectClient(m.policy.PooledClient()), m.tunnels, p.issuer.TunneledMcpServerID)
	if err != nil {
		return nil, ErrFederatedConfiguration
	}
	// The shared exchange reads only these fields; federation owns its own state.
	exchangeState := RemoteLoginState{RedirectURI: callbackURL, CodeVerifier: verifier, TokenEndpoint: p.metadata.TokenEndpoint} //nolint:exhaustruct // Not a persisted remote-session challenge.
	tok, err := m.exchangeCode(ctx, doer, exchangeState, tokenEndpointClientAuth{Method: method, RemoteSessionClientID: p.client.ID, OrganizationID: p.organizationID, JSONWebKeySetID: p.client.JsonWebKeySetID.UUID, ClientID: p.client.ClientID, ClientSecret: secret, AssertionAudience: audience, AssertionSigner: m.assertions}, "", code)
	if err != nil {
		return nil, classifyFederatedExchangeError(err)
	}
	identity, err := m.verifyFederatedIdentity(ctx, p, tok, code, nonce, doer)
	if err != nil {
		return nil, err
	}
	identity.credentials = &EphemeralFederatedCredentials{idToken: tok.IDToken, refreshToken: tok.RefreshToken, expiresIn: tok.ExpiresIn, refreshExpiresIn: tok.RefreshExpiresIn, receivedAt: time.Now()}
	return identity, nil
}

func (m *ChallengeManager) verifyFederatedIdentity(ctx context.Context, p *FederatedProvider, tok tokenResponse, code, nonce string, doer httpDoer) (*FederatedIdentity, error) {
	if tok.IDToken == "" || nonce == "" || m.idTokens == nil {
		return nil, ErrFederatedIdentity
	}
	algorithms, err := acceptedIDTokenAlgorithms(p.metadata.IDTokenSigningAlgValuesSupported)
	if err != nil {
		return nil, ErrFederatedIdentity
	}
	parts := strings.Split(tok.IDToken, ".")
	if len(parts) != 3 {
		return nil, ErrFederatedIdentity
	}
	protected, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrFederatedIdentity
	}
	var protectedFields map[string]json.RawMessage
	if json.Unmarshal(protected, &protectedFields) != nil {
		return nil, ErrFederatedIdentity
	}
	for _, field := range []string{"jwk", "jku", "x5u", "x5c"} {
		if _, present := protectedFields[field]; present {
			return nil, ErrFederatedIdentity
		}
	}
	if b64, present := protectedFields["b64"]; present && string(b64) != "true" {
		return nil, ErrFederatedIdentity
	}
	parsed, err := jwt.ParseSigned(tok.IDToken, algorithms)
	if err != nil || len(parsed.Headers) != 1 {
		return nil, ErrFederatedIdentity
	}
	header := parsed.Headers[0]
	if header.JSONWebKey != nil {
		return nil, ErrFederatedIdentity
	}
	for _, name := range []string{"jku", "x5u", "x5c"} {
		if _, ok := header.ExtraHeaders[jose.HeaderKey(name)]; ok {
			return nil, ErrFederatedIdentity
		}
	}
	identity, err := m.idTokens.Verify(ctx, tok.IDToken, IDTokenExpectation{issuer: p.issuer.Issuer, clientID: p.client.ClientID, jwksURI: p.metadata.JwksURI, fetchScope: p.issuer.ID.String(), transport: doer, signingAlgs: p.metadata.IDTokenSigningAlgValuesSupported, nonce: nonce, subject: ""})
	if err != nil {
		return nil, ErrFederatedIdentity
	}
	return validateFederatedClaims(identity.Claims, p, tok, code, nonce, header.Algorithm, time.Now())
}

func validateFederatedClaims(all map[string]json.RawMessage, p *FederatedProvider, tok tokenResponse, code, nonce, algorithm string, now time.Time) (*FederatedIdentity, error) {
	// Signature verification precedes these stricter OIDC checks. In particular,
	// the enrichment verifier allows normalized issuer and missing iat.
	var claims jwt.Claims
	encoded, err := json.Marshal(all)
	if err != nil || json.Unmarshal(encoded, &claims) != nil {
		return nil, ErrFederatedIdentity
	}
	if claims.Issuer != p.issuer.Issuer || claims.Subject == "" || claims.Expiry == nil || claims.IssuedAt == nil || nonce == "" || claimString(all, "nonce") != nonce {
		return nil, ErrFederatedIdentity
	}
	if claims.ValidateWithLeeway(jwt.Expected{Issuer: p.issuer.Issuer, Subject: "", AnyAudience: jwt.Audience{p.client.ClientID}, ID: "", Time: now}, idTokenMaxSkew) != nil || claims.IssuedAt.Time().After(claims.Expiry.Time()) {
		return nil, ErrFederatedIdentity
	}
	azp := claimString(all, "azp")
	if raw, exists := all["azp"]; exists && (string(raw) == "null" || azp != p.client.ClientID) {
		return nil, ErrFederatedIdentity
	}
	if len(claims.Audience) > 1 && azp != p.client.ClientID {
		return nil, ErrFederatedIdentity
	}
	for name, value := range map[string]string{"at_hash": tok.AccessToken, "c_hash": code} {
		if _, present := all[name]; present && !validFederatedTokenHash(claimString(all, name), value, algorithm) {
			return nil, ErrFederatedIdentity
		}
	}
	email := strings.TrimSpace(claimString(all, "email"))
	if email == "" {
		return nil, ErrFederatedIdentity
	}
	var verified *bool
	if raw, present := all["email_verified"]; present {
		var b bool
		if string(raw) == "null" || json.Unmarshal(raw, &b) != nil {
			return nil, ErrFederatedIdentity
		}
		verified = &b
	}
	return &FederatedIdentity{Issuer: claims.Issuer, Subject: claims.Subject, Email: email, EmailVerified: verified, credentials: nil}, nil
}

func validFederatedTokenHash(claim, value, algorithm string) bool {
	if claim == "" || value == "" {
		return false
	}
	var digest []byte
	switch algorithm {
	case "RS256", "PS256", "ES256":
		sum := sha256.Sum256([]byte(value))
		digest = sum[:]
	case "RS384", "PS384", "ES384":
		sum := sha512.Sum384([]byte(value))
		digest = sum[:]
	case "RS512", "PS512", "ES512":
		sum := sha512.Sum512([]byte(value))
		digest = sum[:]
	default:
		return false
	}
	expected := base64.RawURLEncoding.EncodeToString(digest[:len(digest)/2])
	return subtle.ConstantTimeCompare([]byte(claim), []byte(expected)) == 1
}

var _ fmt.GoStringer = (*FederatedProvider)(nil)

func federatedScopes(issuer repo.RemoteSessionIssuer, client repo.RemoteSessionClient) []string {
	if len(issuer.ScopeOverride) > 0 {
		return issuer.ScopeOverride
	}
	return client.Scope
}

// Preflight only known local availability; never sign or contact KMS at authorize.
func (m *ChallengeManager) preflightFederatedSigner(p *FederatedProvider) error {
	if p.client.TokenEndpointAuthMethod.String == string(TokenEndpointAuthMethodPrivateKeyJWT) && !tokenEndpointSignerAvailable(m.assertions) {
		return ErrFederatedUnavailable
	}
	return nil
}

func classifyFederatedExchangeError(err error) error {
	if _, ok := errors.AsType[*tokenEndpointSigningError](err); ok {
		if errors.Is(err, errTokenEndpointSigningUnavailable) {
			return ErrFederatedUnavailable
		}
		return ErrFederatedSigning
	}
	if _, ok := errors.AsType[*tokenExchangeUnavailableError](err); ok {
		return ErrFederatedUnavailable
	}
	var endpoint *tokenEndpointError
	if errors.As(err, &endpoint) && (endpoint.statusCode >= 500 || endpoint.statusCode == 429) {
		return ErrFederatedUnavailable
	}
	var oauth oautherr.RFC6749Error
	if errors.As(err, &oauth) && (oauth.Code == oautherr.CodeInvalidClient || oauth.Code == "unauthorized_client") {
		return ErrFederatedConfiguration
	}
	return ErrFederatedIdentity
}
