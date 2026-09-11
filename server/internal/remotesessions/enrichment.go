// enrichment.go asks an issuer's own interfaces about a stored credential:
// OpenID Connect UserInfo for who holds it, RFC 7662 introspection for
// whether the provider still honours it. Both are best-effort: an interface
// that does not answer leaves the session as it was and records that it failed.

package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

const (
	// IdentitySourceUserinfo is the identity_source value for an identity the
	// issuer's userinfo endpoint returned for the access token.
	IdentitySourceUserinfo = "userinfo"

	// IdentitySourceIntrospection is the identity_source value for an identity
	// read from an RFC 7662 introspection response.
	IdentitySourceIntrospection = "introspection"

	// IdentitySourceJWTAccessToken is the identity_source value for claims from
	// a verified RFC 9068 access token.
	IdentitySourceJWTAccessToken = "jwt_access_token"

	// interfaceRefreshIntrospection is the enrichment.interfaces key for the
	// refresh-token introspection that backfills refresh_expires_at.
	interfaceRefreshIntrospection = "refresh_introspection"

	// enrichmentInterfaceBudget bounds one call to a userinfo or introspection endpoint.
	enrichmentInterfaceBudget = 4 * time.Second

	// enrichmentVerifyBudget bounds everything a Verify asks the issuer: two
	// interfaces in parallel, the refresh introspection behind one of them.
	enrichmentVerifyBudget = 8 * time.Second

	// enrichmentResponseLimit caps how much of an interface's answer is read.
	enrichmentResponseLimit = 64 << 10

	// introspectionJWTMediaType is the RFC 9701 signed introspection response.
	introspectionJWTMediaType = "application/token-introspection+jwt"

	// introspectionJWTMaxAge bounds how old a signed introspection response
	// may be, since RFC 9701 makes exp optional and iat required.
	introspectionJWTMaxAge = 5 * time.Minute

	interfaceStatusOK       = "ok"
	interfaceStatusFailed   = "failed"
	interfaceStatusLimited  = "limited"
	interfaceStatusRejected = "rejected"

	interfaceReasonNoExp = "no exp"

	tokenTypeHintAccess  = "access_token"
	tokenTypeHintRefresh = "refresh_token"
)

// EnrichmentRate bounds the calls one issuer's enrichment interfaces receive
// from one organization's sessions.
var EnrichmentRate = ratelimit.PerMinute(60)

// identitySourceRank orders the interfaces an identity can come from; only an
// equal or higher rank overwrites the typed identity columns.
var identitySourceRank = map[string]int{
	IdentitySourceIDToken:        3,
	IdentitySourceUserinfo:       2,
	IdentitySourceIntrospection:  1,
	IdentitySourceJWTAccessToken: 0,
}

// overwritableIdentitySources lists the stored identity sources an identity
// from source may replace: itself and everything it outranks. Empty for an
// unranked source, which then never moves the typed columns.
func overwritableIdentitySources(source string) []string {
	rank, ranked := identitySourceRank[source]
	if !ranked {
		return []string{}
	}
	sources := make([]string, 0, len(identitySourceRank))
	for name, r := range identitySourceRank {
		if r <= rank {
			sources = append(sources, name)
		}
	}
	slices.Sort(sources)
	return sources
}

// introspectionRetainedMembers are the RFC 7662 response members kept under enrichment.introspection.
var introspectionRetainedMembers = []string{"active", "exp", "scope", "sub", "aud", "client_id", "username", "sid"}

// jwtAccessTokenRetainedClaims are the access-token claims kept under enrichment.jwt_access_token.
var jwtAccessTokenRetainedClaims = []string{
	"sub", "iss", "aud", "exp", "iat", "nbf", "jti", "client_id", "azp", "scope", "scp",
	"email", "email_verified", "name", "given_name", "family_name", "preferred_username", "sid", "auth_time",
}

// retainMembers keeps only the named members of claims.
func retainMembers(claims map[string]json.RawMessage, names []string) map[string]json.RawMessage {
	kept := make(map[string]json.RawMessage, len(names))
	for _, name := range names {
		if v, ok := claims[name]; ok {
			kept[name] = v
		}
	}
	return kept
}

// enrichmentTarget is what one issuer and client registration contribute to an enrichment call.
type enrichmentTarget struct {
	issuerID              uuid.UUID
	issuerURL             string
	organizationID        string
	userinfoEndpoint      string
	introspectionEndpoint string
	introspectionAuth     []string
	jwksURI               string
	externalClientID      string
	resource              string

	// resourceIndicatorUnsupported is the issuer's explicit false: the resource never reached it.
	resourceIndicatorUnsupported bool

	clientSecretEncrypted string
	tokenEndpointAuth     string
}

func enrichmentTargetFromClient(row remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow, organizationID string) enrichmentTarget {
	return enrichmentTarget{
		issuerID:                     row.RemoteSessionIssuerID,
		issuerURL:                    row.IssuerUrl,
		organizationID:               organizationID,
		userinfoEndpoint:             conv.FromPGTextOrEmpty[string](row.UserinfoEndpoint),
		introspectionEndpoint:        conv.FromPGTextOrEmpty[string](row.IntrospectionEndpoint),
		introspectionAuth:            row.IntrospectionEndpointAuthMethodsSupported,
		jwksURI:                      conv.FromPGTextOrEmpty[string](row.JwksUri),
		externalClientID:             row.ExternalClientID,
		resource:                     "",
		resourceIndicatorUnsupported: row.ResourceIndicatorSupported.Valid && !row.ResourceIndicatorSupported.Bool,
		clientSecretEncrypted:        conv.FromPGTextOrEmpty[string](row.ClientSecretEncrypted),
		tokenEndpointAuth:            conv.FromPGTextOrEmpty[string](row.TokenEndpointAuthMethod),
	}
}

// interfaceRecord is the last outcome of one enrichment interface, stored
// under enrichment.interfaces so a surface can tell "never asked" from
// "asked and failed".
type interfaceRecord struct {
	// Status is interfaceStatusOK, interfaceStatusFailed, or interfaceStatusLimited.
	Status string `json:"status"`

	// At is when the interface was called.
	At time.Time `json:"at"`

	// HTTPStatus is the response status when the interface answered at all.
	HTTPStatus int `json:"http_status,omitempty"`

	// Reason is a fixed-phrase failure summary; nothing the provider sent.
	Reason string `json:"reason,omitempty"`
}

func (r *interfaceRecord) fail(reason string) {
	r.Status, r.Reason = interfaceStatusFailed, reason
}

// interfaceResult is what one enrichment call reports about itself.
type interfaceResult struct {
	// ran is false when the issuer advertises no endpoint; the record is then meaningless.
	ran bool
	interfaceRecord
}

func (r interfaceResult) ok() bool { return r.ran && r.Status == interfaceStatusOK }

// failed reports a call that reached the wire, or could not, and produced no usable answer.
func (r interfaceResult) failed() bool { return r.ran && r.Status == interfaceStatusFailed }

// userinfoResult is a userinfo call; identity is set only when ok.
type userinfoResult struct {
	interfaceResult
	identity *UpstreamIdentity
}

// jwtAccessTokenResult is local verified-access-token enrichment. It embeds
// the same outcome contract as issuer interfaces so persistence and privacy
// handling stay uniform.
type jwtAccessTokenResult struct {
	interfaceResult
	identity     *UpstreamIdentity
	claims       map[string]json.RawMessage
	scopes       []string
	scopePresent bool
}

// retained returns the allowlisted claims of a verified token, whether or not its identity was selected.
func (r jwtAccessTokenResult) retained() map[string]json.RawMessage {
	if !r.ok() {
		return nil
	}
	return r.claims
}

// introspectionResult is an introspection call; active and the members are
// meaningful only when ok, identity only when active and a subject was named.
type introspectionResult struct {
	interfaceResult
	active bool

	// members are the retained response members, for enrichment.introspection.
	members map[string]json.RawMessage

	// expiresAt is the exp member; zero when absent.
	expiresAt time.Time
	identity  *UpstreamIdentity
}

// interfaceAnswer is what an enrichment endpoint sent back.
type interfaceAnswer struct {
	status    int
	mediaType string
	body      []byte
}

// SessionEnricher calls an issuer's userinfo and introspection endpoints.
// Safe for concurrent use.
type SessionEnricher struct {
	logger *slog.Logger
	enc    *encryption.Client

	// client is shared by every call, the same pooled transport the revoker holds.
	client *guardian.HTTPClient

	// limiter paces calls per issuer and organization; nil leaves them unpaced, which only tests want.
	limiter *ratelimit.Limiter

	// keys verifies RFC 9701 signed introspection responses; nil rejects them.
	keys *jwks.KeyResolver

	// requestIssuerMetadataRefresh is called when an advertised endpoint
	// answers 404, so the issuer's discovery document can be re-read. AIM-260
	// supplies the refresh; until then the request is dropped.
	requestIssuerMetadataRefresh func(ctx context.Context, issuerID uuid.UUID)
}

// NewSessionEnricher binds the transport, client-secret decryption, per-issuer
// budget, and key resolver an enrichment call needs.
func NewSessionEnricher(logger *slog.Logger, enc *encryption.Client, policy *guardian.Policy, keys *jwks.KeyResolver, limiter *ratelimit.Limiter) *SessionEnricher {
	return &SessionEnricher{
		logger:                       logger.With(attr.SlogComponent("remote-session-enrichment")),
		enc:                          enc,
		client:                       noRedirectClient(policy.PooledClient()),
		limiter:                      limiter,
		keys:                         keys,
		requestIssuerMetadataRefresh: func(context.Context, uuid.UUID) {},
	}
}

// noRedirectClient copies base and refuses redirects: a bearer or client secret
// rides these requests, and a provider-chosen redirect must not carry it elsewhere.
func noRedirectClient(base *guardian.HTTPClient) *guardian.HTTPClient {
	client := *base
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

// introspectionAuthMethod picks how to authenticate at the introspection
// endpoint: the first advertised method the held credentials satisfy, else
// the token endpoint's own method, which is what providers that only accept
// the registered method enforce.
func introspectionAuthMethod(advertised []string, tokenEndpointMethod string, clientSecret string) (TokenEndpointAuthMethod, error) {
	for _, name := range advertised {
		switch method := TokenEndpointAuthMethod(name); method {
		case TokenEndpointAuthMethodBasic, TokenEndpointAuthMethodPost:
			if clientSecret != "" {
				return method, nil
			}
		case TokenEndpointAuthMethodNone:
			if clientSecret == "" {
				return method, nil
			}
		case TokenEndpointAuthMethodPrivateKeyJWT:
		}
	}
	return ResolveTokenEndpointAuthMethod(tokenEndpointMethod, clientSecret)
}

// run performs one enrichment call: the endpoint and budget gates, the request build, and the read.
func (e *SessionEnricher) run(ctx context.Context, target enrichmentTarget, name, endpoint string, build func(context.Context) (*http.Request, error)) (interfaceAnswer, interfaceResult) {
	var none interfaceAnswer
	res := interfaceResult{ran: false, interfaceRecord: interfaceRecord{Status: interfaceStatusFailed, At: time.Now(), HTTPStatus: 0, Reason: ""}}
	if endpoint == "" {
		return none, res
	}
	res.ran = true
	logger := e.logger.With(attr.SlogOAuthIssuer(target.issuerURL), attr.SlogOAuthFlowStage(name))
	if !usableUpstreamEndpoint(ctx, logger, name, endpoint) {
		res.fail("unusable endpoint")
		return none, res
	}
	if e.limiter != nil {
		allowed, err := e.limiter.Allow(ctx, target.issuerID.String()+":"+target.organizationID)
		switch {
		case err != nil:
			logger.WarnContext(ctx, "enrichment limiter unavailable; allowing", attr.SlogError(err))
		case !allowed.Allowed:
			logger.DebugContext(ctx, "enrichment interface rate limited")
			res.Status, res.Reason = interfaceStatusLimited, "rate limited"
			return none, res
		}
	}

	ctx, cancel := context.WithTimeout(ctx, enrichmentInterfaceBudget)
	defer cancel()

	req, err := build(ctx)
	if err != nil {
		logger.WarnContext(ctx, "enrichment request not built", attr.SlogError(err))
		res.fail("request not built")
		return none, res
	}
	resp, err := e.client.Do(req)
	if err != nil {
		logger.WarnContext(ctx, "enrichment interface unreachable", attr.SlogError(err))
		res.fail("unreachable")
		return none, res
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	res.HTTPStatus = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, enrichmentResponseLimit))
	if err != nil {
		res.fail("response unreadable")
		return none, res
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// The advertised endpoint has moved; discovery needs re-reading.
		logger.WarnContext(ctx, "enrichment interface answered 404; issuer metadata refresh requested")
		e.requestIssuerMetadataRefresh(ctx, target.issuerID)
		res.fail("status 404")
		return none, res
	default:
		res.fail(fmt.Sprintf("status %d", resp.StatusCode))
		return none, res
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	res.Status = interfaceStatusOK
	return interfaceAnswer{status: resp.StatusCode, mediaType: mediaType, body: body}, res
}

// userinfo presents the access token to the issuer's userinfo endpoint. A
// usable answer is a 200 whose JSON body names a sub; Slack, for one, answers
// 200 with an error body, which is a failure here.
func (e *SessionEnricher) userinfo(ctx context.Context, target enrichmentTarget, accessToken string) userinfoResult {
	answer, res := e.run(ctx, target, IdentitySourceUserinfo, target.userinfoEndpoint, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.userinfoEndpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build userinfo request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	out := userinfoResult{interfaceResult: res, identity: nil}
	if !res.ok() {
		return out
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(answer.body, &claims); err != nil {
		out.fail("not a json object")
		return out
	}
	subject := claimString(claims, "sub")
	if subject == "" {
		out.fail("no sub")
		return out
	}
	out.identity = &UpstreamIdentity{
		Subject:     subject,
		Email:       claimString(claims, "email"),
		DisplayName: displayNameFromClaims(claims),
		Source:      IdentitySourceUserinfo,
		Claims:      claims,
	}
	return out
}

// jwtAccessToken verifies signed access-token claims. Explicit access-token
// types require RFC 9068 claims; Gram's generic-type compatibility path
// additionally requires a recorded resource distinct from the OAuth client.
// ran is false when nothing was examined: an opaque token, an issuer with no
// key set, or a key set that could not be consulted.
func (e *SessionEnricher) jwtAccessToken(ctx context.Context, target enrichmentTarget, accessToken string) jwtAccessTokenResult {
	now := time.Now()
	out := jwtAccessTokenResult{
		interfaceResult: interfaceResult{interfaceRecord: interfaceRecord{Status: interfaceStatusFailed, HTTPStatus: 0, Reason: "", At: now}, ran: false},
		identity:        nil,
		claims:          nil,
		scopes:          nil,
		scopePresent:    false,
	}
	if strings.Count(accessToken, ".") != 2 || e.keys == nil || target.jwksURI == "" {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, idTokenVerifyBudget)
	defer cancel()
	var claims jwt.Claims
	var all map[string]json.RawMessage
	header, err := verifyIssuerSignedJWTWithKeyPolicy(ctx, e.keys, target.jwksURI, target.issuerID.String(), accessToken, jwks.AllowedSignatureAlgorithms(), validateJWTVerificationKeyStrength, &claims, &all)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, errJWTKeySetUnavailable) {
			return out
		}
		out.ran = true
		e.rejectJWTAccessToken(ctx, target, &out, "unverifiable token")
		return out
	}
	out.ran = true
	// RFC 9068 §2.1: typ distinguishes the access-token profile.
	typ, typePresent, err := jwtAccessTokenType(header)
	if err != nil {
		e.rejectJWTAccessToken(ctx, target, &out, "invalid token type")
		return out
	}
	typ = strings.ToLower(typ)
	dedicatedType := typ == "at+jwt" || typ == "application/at+jwt"
	// Gram compatibility: generic/missing typ is only accepted for a distinct resource audience.
	genericType := !typePresent || typ == "jwt" || typ == "application/jwt"
	if !dedicatedType && !genericType {
		e.rejectJWTAccessToken(ctx, target, &out, "wrong token type")
		return out
	}
	// A resource the issuer never saw cannot be the token's audience.
	resource := target.resource
	if target.resourceIndicatorUnsupported {
		resource = ""
	}
	// Gram compatibility: without a recorded resource, a dedicated token may use the client audience.
	expectedAudience := conv.Default(resource, target.externalClientID)
	if genericType && (resource == "" || resource == target.externalClientID) {
		e.rejectJWTAccessToken(ctx, target, &out, "ambiguous token type")
		return out
	}
	// RFC 9068 §2.2: iss, sub, aud, and exp are required.
	if claims.Issuer == "" || claims.Subject == "" || claims.Expiry == nil {
		e.rejectJWTAccessToken(ctx, target, &out, "missing required claims")
		return out
	}
	// RFC 9068 §4: require the expected resource audience (Gram fallback above).
	if expectedAudience == "" || !claims.Audience.Contains(expectedAudience) {
		e.rejectJWTAccessToken(ctx, target, &out, "audience mismatch")
		return out
	}
	// RFC 9068 §4 requires the issuer identifier to match exactly.
	if claims.Issuer != target.issuerURL {
		e.rejectJWTAccessToken(ctx, target, &out, "invalid claims")
		return out
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Issuer: "", Subject: "", AnyAudience: nil, ID: "", Time: now}, idTokenMaxSkew); err != nil {
		e.rejectJWTAccessToken(ctx, target, &out, "invalid claims")
		return out
	}
	if dedicatedType {
		if err := validateRFC9068Claims(claims, all, target.externalClientID); err != nil {
			e.rejectJWTAccessToken(ctx, target, &out, err.Error())
			return out
		}
	} else {
		// Gram binding policy: a client the token does name must be this OAuth client.
		for _, name := range []string{"client_id", "azp"} {
			if _, present := all[name]; present && claimString(all, name) != target.externalClientID {
				e.rejectJWTAccessToken(ctx, target, &out, "client mismatch")
				return out
			}
		}
	}
	if raw, ok := all["scope"]; ok {
		out.scopePresent = true
		var scope *string
		if json.Unmarshal(raw, &scope) != nil || scope == nil {
			e.rejectJWTAccessToken(ctx, target, &out, "invalid scope")
			return out
		}
		// RFC 6749 §3.3: scope-token syntax separated by single spaces.
		out.scopes, err = parseJWTAccessTokenScope(*scope)
		if err != nil {
			e.rejectJWTAccessToken(ctx, target, &out, "invalid scope")
			return out
		}
	}
	kept := retainMembers(all, jwtAccessTokenRetainedClaims)
	out.Status = interfaceStatusOK
	out.claims = kept
	out.identity = &UpstreamIdentity{
		Subject: claims.Subject, Email: claimString(kept, "email"),
		DisplayName: displayNameFromClaims(kept), Source: IdentitySourceJWTAccessToken, Claims: kept,
	}
	return out
}

// jwtAccessTokenType reads typ from a verified header; present is false when the header carries none.
func jwtAccessTokenType(header jose.Header) (typ string, present bool, err error) {
	// RFC 7797 §7: JWTs MUST NOT use the unencoded-payload option, which go-jose would otherwise accept.
	if rawB64, ok := header.ExtraHeaders[jose.HeaderKey("b64")]; ok {
		if encoded, isBool := rawB64.(bool); !isBool || !encoded {
			return "", false, errors.New("jwt requires a base64url-encoded payload")
		}
	}
	rawType, ok := header.ExtraHeaders[jose.HeaderType]
	if !ok {
		return "", false, nil
	}
	typ, isString := rawType.(string)
	if !isString {
		return "", true, errors.New("invalid jwt typ")
	}
	return typ, true, nil
}

func (e *SessionEnricher) rejectJWTAccessToken(ctx context.Context, target enrichmentTarget, out *jwtAccessTokenResult, reason string) {
	out.fail(reason)
	logIdentityFailure(ctx, e.logger, "jwt access token rejected", errors.New(reason), attr.SlogOAuthIssuer(target.issuerURL))
}

// introspect presents a token to the issuer's introspection endpoint,
// authenticating as the client the way the token endpoint does. Only a 200
// carrying an active member is a usable answer: a 403 or a body without one
// says nothing about the token.
func (e *SessionEnricher) introspect(ctx context.Context, target enrichmentTarget, token string, hint string) introspectionResult {
	answer, res := e.run(ctx, target, IdentitySourceIntrospection, target.introspectionEndpoint, func(ctx context.Context) (*http.Request, error) {
		var clientSecret string
		if target.clientSecretEncrypted != "" {
			plain, err := e.enc.Decrypt(target.clientSecretEncrypted)
			if err != nil {
				return nil, fmt.Errorf("decrypt client secret: %w", err)
			}
			clientSecret = plain
		}
		authMethod, err := introspectionAuthMethod(target.introspectionAuth, target.tokenEndpointAuth, clientSecret)
		if err != nil {
			return nil, fmt.Errorf("resolve client auth: %w", err)
		}
		form := url.Values{}
		form.Set("token", token)
		form.Set("token_type_hint", hint)
		req, err := newTokenEndpointRequest(ctx, target.introspectionEndpoint, form, authMethod, target.externalClientID, clientSecret)
		if err != nil {
			return nil, err
		}
		// A signed response is only worth asking for when it can be verified.
		if e.keys != nil && target.jwksURI != "" {
			req.Header.Set("Accept", "application/json, "+introspectionJWTMediaType)
		}
		return req, nil
	})
	out := introspectionResult{interfaceResult: res, active: false, members: nil, expiresAt: time.Time{}, identity: nil}
	if !res.ok() {
		return out
	}
	members, err := e.decodeIntrospection(ctx, target, answer)
	if err != nil {
		logIdentityFailure(ctx, e.logger, "introspection response rejected", err, attr.SlogOAuthIssuer(target.issuerURL))
		out.fail("unverifiable response")
		return out
	}
	var active *bool
	if raw, present := members["active"]; !present || json.Unmarshal(raw, &active) != nil || active == nil {
		out.fail("no active member")
		return out
	}
	out.active = *active
	out.members = retainMembers(members, introspectionRetainedMembers)
	var exp int64
	if raw, ok := members["exp"]; ok && json.Unmarshal(raw, &exp) == nil && exp > 0 {
		out.expiresAt = time.Unix(exp, 0)
	}
	if subject := claimString(members, "sub"); out.active && subject != "" {
		out.identity = &UpstreamIdentity{
			Subject:     subject,
			Email:       "",
			DisplayName: claimString(members, "username"),
			Source:      IdentitySourceIntrospection,
			Claims:      out.members,
		}
	}
	return out
}

// decodeIntrospection reads a plain JSON response, or verifies an RFC 9701
// signed one against the issuer's key set before trusting its members.
func (e *SessionEnricher) decodeIntrospection(ctx context.Context, target enrichmentTarget, answer interfaceAnswer) (map[string]json.RawMessage, error) {
	if answer.mediaType != introspectionJWTMediaType {
		var members map[string]json.RawMessage
		if err := json.Unmarshal(answer.body, &members); err != nil {
			return nil, fmt.Errorf("decode introspection response: %w", err)
		}
		return members, nil
	}
	if e.keys == nil || target.jwksURI == "" {
		return nil, errors.New("signed introspection response cannot be verified without the issuer's key set")
	}
	var claims jwt.Claims
	var envelope struct {
		TokenIntrospection map[string]json.RawMessage `json:"token_introspection"`
	}
	header, err := verifyIssuerSignedJWT(ctx, e.keys, target.jwksURI, target.issuerID.String(), strings.TrimSpace(string(answer.body)), jwks.AllowedSignatureAlgorithms(), &claims, &envelope)
	if err != nil {
		return nil, err
	}
	// RFC 9701 §4: typ names the response so a JWT minted for another purpose cannot be replayed as one.
	if typ, _ := header.ExtraHeaders[jose.HeaderType].(string); !strings.EqualFold(strings.TrimPrefix(typ, "application/"), "token-introspection+jwt") {
		return nil, errors.New("introspection jwt typ is not token-introspection+jwt")
	}
	if !issuerURLsEqual(claims.Issuer, target.issuerURL) {
		return nil, fmt.Errorf("introspection jwt issuer %q is not the grant's issuer", truncateForMessage(claims.Issuer))
	}
	now := time.Now()
	if claims.IssuedAt == nil {
		return nil, errors.New("introspection jwt has no iat")
	}
	if now.Sub(claims.IssuedAt.Time()) > introspectionJWTMaxAge+idTokenMaxSkew {
		return nil, errors.New("introspection jwt is too old")
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      "",
		Subject:     "",
		AnyAudience: jwt.Audience{target.externalClientID},
		ID:          "",
		Time:        now,
	}, idTokenMaxSkew); err != nil {
		return nil, fmt.Errorf("validate introspection jwt claims: %w", err)
	}
	if envelope.TokenIntrospection == nil {
		return nil, errors.New("introspection jwt has no token_introspection member")
	}
	return envelope.TokenIntrospection, nil
}

// usableUpstreamEndpoint rejects an issuer-advertised endpoint the transport
// must not be pointed at: tokens travel on it, so only https, or http on
// loopback, is accepted. The guardian policy remains the SSRF control.
func usableUpstreamEndpoint(ctx context.Context, logger *slog.Logger, kind, endpoint string) bool {
	if urls.IsAbsoluteHTTPSOrLoopback(endpoint) {
		return true
	}
	logger.WarnContext(ctx, "issuer advertises an unusable "+kind+" endpoint",
		attr.SlogOAuthFailureReason(kind+" endpoint must be an absolute https url, or http on loopback"),
	)
	return false
}

// UpstreamVerification is what the issuer's own interfaces said about a credential on Verify.
type UpstreamVerification struct {
	// Inactive is true only when introspection answered active:false for the access token.
	Inactive bool
}

// EnrichRemoteSession runs the grant's enrichment interfaces on Verify and
// stores what they said: the typed identity from the highest-ranked source
// that answered, each source's members under its own key (null once the
// interface fails, so a stale answer never outlives it), and a refresh
// deadline introspection reported that the exchange did not. Userinfo is
// skipped when an ID token identity is stored, since it could not move the
// typed columns. Nothing is written when the grant moved since ref was read.
func (m *ChallengeManager) EnrichRemoteSession(ctx context.Context, ref RemoteSessionRef) (UpstreamVerification, error) {
	var none UpstreamVerification
	q := remotesessions_repo.New(m.db)
	if _, err := q.GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             ref.ClientID,
		ProjectID:      ref.ProjectID,
		OrganizationID: ref.OrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return none, nil
		}
		return none, fmt.Errorf("load remote_session_client for enrichment: %w", err)
	}
	sess, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            ref.Subject,
		RemoteSessionClientID: ref.ClientID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (sess.ID != ref.ID || !sess.UpdatedAt.Time.Equal(ref.UpdatedAt))) {
		return none, nil
	}
	if err != nil {
		return none, fmt.Errorf("load remote session for enrichment: %w", err)
	}
	client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, sess.RemoteSessionClientID)
	if err != nil {
		return none, fmt.Errorf("load issuer for enrichment: %w", err)
	}
	target := enrichmentTargetFromClient(client, ref.OrganizationID)
	target.resource = conv.FromPGTextOrEmpty[string](sess.Resource)
	if sess.IdentitySource.Valid && !slices.Contains(overwritableIdentitySources(IdentitySourceUserinfo), sess.IdentitySource.String) {
		target.userinfoEndpoint = ""
	}
	accessToken, err := m.enc.Decrypt(sess.AccessTokenEncrypted)
	if err != nil {
		return none, fmt.Errorf("decrypt access token for enrichment: %w", err)
	}
	attrs := []slog.Attr{
		attr.SlogOAuthIssuer(client.IssuerUrl),
		attr.SlogRemoteSessionID(sess.ID.String()),
		attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()),
	}

	callCtx, cancel := context.WithTimeout(ctx, enrichmentVerifyBudget)
	defer cancel()
	var userinfo userinfoResult
	var access jwtAccessTokenResult
	var introspection introspectionResult
	var refresh *interfaceRecord
	var calls sync.WaitGroup
	calls.Go(func() { userinfo = m.enricher.userinfo(callCtx, target, accessToken) })
	calls.Go(func() {
		introspection = m.enricher.introspect(callCtx, target, accessToken, tokenTypeHintAccess)
		refresh = m.backfillRefreshDeadline(callCtx, q, ref, sess, target, introspection, attrs)
	})
	calls.Wait()

	// The highest-ranked source that answered writes the typed columns. A
	// different subject is someone else's identity (§12.2): nothing it said
	// is kept. A grant whose ID token the exchange rejected takes no identity
	// from a weaker source either: the rejection stands until a reconnect.
	for _, candidate := range []struct {
		subject string
		res     *interfaceResult
	}{
		{subject: identitySubject(userinfo.identity), res: &userinfo.interfaceResult},
		{subject: identitySubject(introspection.identity), res: &introspection.interfaceResult},
	} {
		if candidate.subject != "" && sess.UpstreamSubject.Valid && candidate.subject != sess.UpstreamSubject.String {
			logIdentityFailure(ctx, m.logger, "enrichment identity rejected; stored identity kept", errIDTokenSubjectMismatch, attrs...)
			candidate.res.fail("subject mismatch")
		}
	}
	stored := storedInterfaceRecords(sess.Enrichment)
	idTokenRejected := stored[IdentitySourceIDToken].Status == interfaceStatusRejected
	strongerIdentity := (userinfo.ok() && userinfo.identity != nil) || (introspection.ok() && introspection.identity != nil)
	jwtCanWrite := !sess.IdentitySource.Valid || slices.Contains(overwritableIdentitySources(IdentitySourceJWTAccessToken), sess.IdentitySource.String)
	if !idTokenRejected && !strongerIdentity && jwtCanWrite {
		access = m.enricher.jwtAccessToken(callCtx, target, accessToken)
		if subject := identitySubject(access.identity); subject != "" && sess.UpstreamSubject.Valid && subject != sess.UpstreamSubject.String {
			m.enricher.rejectJWTAccessToken(ctx, target, &access, "subject mismatch")
			access.identity = nil
		}
	}
	if !userinfo.ran && !access.ran && !introspection.ran {
		return none, nil
	}
	var identity *UpstreamIdentity
	interfaces := map[string]interfaceRecord{}
	doc := map[string]any{"interfaces": interfaces}
	if userinfo.ran {
		interfaces[IdentitySourceUserinfo] = userinfo.interfaceRecord
	}
	switch {
	case userinfo.ok():
		doc[IdentitySourceUserinfo] = retainedClaims(userinfo.identity.Claims)
		identity = userinfo.identity
	case userinfo.failed():
		doc[IdentitySourceUserinfo] = nil
	}
	if introspection.ran {
		interfaces[IdentitySourceIntrospection] = introspection.interfaceRecord
	}
	switch {
	case introspection.ok():
		doc[IdentitySourceIntrospection] = introspection.members
		if identity == nil {
			identity = introspection.identity
		}
	case introspection.failed():
		doc[IdentitySourceIntrospection] = nil
	}
	if access.ran {
		interfaces[IdentitySourceJWTAccessToken] = access.interfaceRecord
	}
	switch {
	case access.ok():
		doc[IdentitySourceJWTAccessToken] = retainedClaims(access.identity.Claims)
		if identity == nil {
			identity = access.identity
		}
	case access.failed():
		doc[IdentitySourceJWTAccessToken] = nil
	}
	if refresh != nil {
		interfaces[interfaceRefreshIntrospection] = *refresh
	}
	if idTokenRejected && identity != nil {
		logIdentityFailure(ctx, m.logger, "enrichment identity withheld; the exchange rejected the grant's id token", errors.New("id token rejected at exchange"), attrs...)
		identity = nil
	}
	enrichment, err := json.Marshal(doc)
	if err != nil {
		return none, fmt.Errorf("marshal enrichment document: %w", err)
	}
	if len(enrichment) > maxEnrichmentBytes {
		// The answers are dropped; the record of having asked and the nulls retiring stale answers are kept.
		logIdentityFailure(ctx, m.logger, "enrichment answers dropped; stored document kept", errEnrichmentTooLarge, attrs...)
		for name, value := range doc {
			if value != nil && name != "interfaces" {
				delete(doc, name)
			}
		}
		enrichment, err = json.Marshal(doc)
		if err != nil {
			return none, fmt.Errorf("marshal enrichment interfaces: %w", err)
		}
	}

	cols := identity.columns()
	_, err = q.UpdateRemoteSessionIdentity(ctx, remotesessions_repo.UpdateRemoteSessionIdentityParams{
		Scopes:                nil,
		OverwritableSources:   overwritableIdentitySources(cols.Source.String),
		UpstreamSubject:       cols.Subject,
		UpstreamEmail:         cols.Email,
		UpstreamDisplayName:   cols.DisplayName,
		IdentitySource:        cols.Source,
		Enrichment:            enrichment,
		ID:                    sess.ID,
		SubjectUrn:            sess.SubjectUrn,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		ExpectedUpdatedAt:     sess.UpdatedAt,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, fmt.Errorf("persist remote session enrichment: %w", err)
	}
	return UpstreamVerification{Inactive: introspection.ok() && !introspection.active}, nil
}

func identitySubject(identity *UpstreamIdentity) string {
	if identity == nil {
		return ""
	}
	return identity.Subject
}

// backfillRefreshDeadline fills a refresh deadline the exchange did not
// report by introspecting the refresh token, once per grant: a provider that
// answered without exp, or already answered, is not asked again. The record
// to store is returned; nil when nothing was asked.
func (m *ChallengeManager) backfillRefreshDeadline(ctx context.Context, q *remotesessions_repo.Queries, ref RemoteSessionRef, sess remotesessions_repo.RemoteSession, target enrichmentTarget, access introspectionResult, attrs []slog.Attr) *interfaceRecord {
	if !access.ok() || !hasRefreshToken(sess) || sess.RefreshExpiresAt.Valid {
		return nil
	}
	if prior, asked := storedInterfaceRecords(sess.Enrichment)[interfaceRefreshIntrospection]; asked && (prior.Status == interfaceStatusOK || prior.Reason == interfaceReasonNoExp) {
		return nil
	}
	refreshToken, err := m.enc.Decrypt(sess.RefreshTokenEncrypted.String)
	if err != nil {
		return nil
	}
	refresh := m.enricher.introspect(ctx, target, refreshToken, tokenTypeHintRefresh)
	record := refresh.interfaceRecord
	switch {
	case !refresh.ok():
		return &record
	case !refresh.active || refresh.expiresAt.IsZero():
		record.fail(interfaceReasonNoExp)
		return &record
	}
	if _, err := q.SetRemoteSessionRefreshExpiresAtIfUnknown(ctx, remotesessions_repo.SetRemoteSessionRefreshExpiresAtIfUnknownParams{
		RefreshExpiresAt:      conv.PtrToPGTimestamptz(&refresh.expiresAt),
		ID:                    sess.ID,
		SubjectUrn:            sess.SubjectUrn,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		ExpectedUpdatedAt:     sess.UpdatedAt,
		ProjectID:             ref.ProjectID,
		OrganizationID:        ref.OrganizationID,
	}); err != nil {
		m.logger.LogAttrs(ctx, slog.LevelError, "persist introspected refresh deadline", append([]slog.Attr{attr.SlogError(err)}, attrs...)...)
	}
	return &record
}

// storedInterfaceRecords reads the interface records already on the session.
func storedInterfaceRecords(stored []byte) map[string]interfaceRecord {
	var doc enrichmentDocument
	if len(stored) > 0 {
		_ = json.Unmarshal(stored, &doc)
	}
	return doc.Interfaces
}
