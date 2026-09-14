package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// IdentitySourceIDToken is the identity_source value for an identity taken
// from a verified OpenID Connect ID token returned by the token endpoint.
const IdentitySourceIDToken = "id_token"

// idTokenMaxSkew is the clock difference tolerated on an ID token's temporal
// claims, in both directions.
const idTokenMaxSkew = time.Minute

// idTokenVerifyBudget bounds one verification including the key fetch; past it
// the session is stored without identity.
const idTokenVerifyBudget = 5 * time.Second

// errIDTokenSubjectMismatch reports an ID token for someone other than the
// identity the session carries.
var errIDTokenSubjectMismatch = errors.New("id token subject differs from the identity the session carries")

// errIDTokenVerificationDisabled is NoIDTokenVerifier's answer; callers treat it as silence.
var errIDTokenVerificationDisabled = errors.New("id token verification is disabled")

// maxEnrichmentBytes caps the provider-controlled enrichment document; larger
// ones are dropped. UpdateRemoteSessionIdentity repeats it as the 16384 literal.
const maxEnrichmentBytes = 16 << 10

// errEnrichmentTooLarge reports an enrichment document over maxEnrichmentBytes.
var errEnrichmentTooLarge = errors.New("enrichment document exceeds the size limit")

var (
	// IDTokenKeyRefreshRate bounds how often one issuer's key set is re-fetched
	// because an ID token named an unknown kid.
	IDTokenKeyRefreshRate = ratelimit.PerMinute(10)

	// IDTokenKeyFetchRate bounds every upstream key set consult an issuer
	// causes, cold or forced.
	IDTokenKeyFetchRate = ratelimit.PerMinute(30)
)

// UpstreamIdentity is who a grant belongs to at the provider; unstated fields are zero.
type UpstreamIdentity struct {
	// Subject is the provider's stable identifier for the user.
	Subject string

	// Email is the user's email as the issuer asserted it, not necessarily
	// verified: display-only, it never keys a grant or an authorization decision.
	Email string

	// DisplayName is the best human-readable name the interface offered:
	// name, then given and family name, then a preferred username. Display-only, like Email.
	DisplayName string

	// Source is the identity_source value naming the interface.
	Source string

	// Claims is every claim returned; enrichment keeps them minus credential-shaped members.
	Claims map[string]json.RawMessage
}

// identityColumns is an identity projected onto the nullable remote_sessions
// identity columns: NULL wherever the identity is nil or the field is empty.
type identityColumns struct {
	Subject     pgtype.Text
	Email       pgtype.Text
	DisplayName pgtype.Text
	Source      pgtype.Text
}

// columns projects the identity onto its columns; a nil identity is all NULL.
func (i *UpstreamIdentity) columns() identityColumns {
	if i == nil {
		return identityColumns{
			Subject:     pgtype.Text{String: "", Valid: false},
			Email:       pgtype.Text{String: "", Valid: false},
			DisplayName: pgtype.Text{String: "", Valid: false},
			Source:      pgtype.Text{String: "", Valid: false},
		}
	}
	return identityColumns{
		Subject:     conv.ToPGTextEmpty(i.Subject),
		Email:       conv.ToPGTextEmpty(i.Email),
		DisplayName: conv.ToPGTextEmpty(i.DisplayName),
		Source:      conv.ToPGTextEmpty(i.Source),
	}
}

// IDTokenVerifier verifies an ID token and returns the identity it asserts. Safe for concurrent use.
type IDTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string, expect IDTokenExpectation) (UpstreamIdentity, error)
}

// jwksIDTokenVerifier verifies ID tokens against the issuer's published key
// set.
type jwksIDTokenVerifier struct {
	keys *jwks.KeyResolver
}

// NewIDTokenVerifier binds the key resolver an ID token verification needs.
func NewIDTokenVerifier(keys *jwks.KeyResolver) IDTokenVerifier {
	return &jwksIDTokenVerifier{keys: keys}
}

// noIDTokenVerifier answers every token with errIDTokenVerificationDisabled.
type noIDTokenVerifier struct{}

// NoIDTokenVerifier is the verifier for deployments that capture no
// identity: sessions are stored without one and nothing is logged.
func NoIDTokenVerifier() IDTokenVerifier {
	return noIDTokenVerifier{}
}

func (noIDTokenVerifier) Verify(context.Context, string, IDTokenExpectation) (UpstreamIdentity, error) {
	return UpstreamIdentity{}, errIDTokenVerificationDisabled
}

// NewIDTokenKeyResolver builds the resolver with refresh and fetch budgets charged to store.
func NewIDTokenKeyResolver(logger *slog.Logger, policy *guardian.Policy, meterProvider metric.MeterProvider, store ratelimit.Store) (*jwks.KeyResolver, error) {
	refreshLimiter := ratelimit.New(store, "remote_session_id_token_jwks_refresh", IDTokenKeyRefreshRate)
	fetchLimiter := ratelimit.New(store, "remote_session_id_token_jwks_fetch", IDTokenKeyFetchRate)
	keys, err := jwks.NewKeyResolver(jwks.NewResolver(policy, meterProvider, logger), jwks.NewMemoryCache(), refreshLimiter, fetchLimiter, logger)
	if err != nil {
		return nil, fmt.Errorf("new id token key resolver: %w", err)
	}
	return keys, nil
}

// IDTokenExpectation is what a verified ID token must say. Nonce is empty on
// a refresh (§12.2); subject is the stored identity a refresh must restate.
type IDTokenExpectation struct {
	issuer   string
	clientID string
	jwksURI  string

	// fetchScope is the issuer row id, so tenants sharing an issuer URL cannot spend each other's budget.
	fetchScope string

	// signingAlgs is the issuer's id_token_signing_alg_values_supported; empty means unadvertised.
	signingAlgs []string

	nonce   string
	subject string
}

// acceptedIDTokenAlgorithms narrows the shared allowlist to what the issuer advertises, when it advertises anything.
func acceptedIDTokenAlgorithms(advertised []string) ([]jose.SignatureAlgorithm, error) {
	allowed := jwks.AllowedSignatureAlgorithms()
	if len(advertised) == 0 {
		return allowed, nil
	}
	accepted := make([]jose.SignatureAlgorithm, 0, len(allowed))
	for _, alg := range allowed {
		if slices.Contains(advertised, string(alg)) {
			accepted = append(accepted, alg)
		}
	}
	if len(accepted) == 0 {
		return nil, errors.New("issuer advertises no id token signing algorithm this verifier accepts")
	}
	return accepted, nil
}

// Verify checks an ID token per OpenID Connect Core §3.1.3.7; the raw token is never retained or logged.
func (v *jwksIDTokenVerifier) Verify(ctx context.Context, rawIDToken string, expect IDTokenExpectation) (UpstreamIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, idTokenVerifyBudget)
	defer cancel()

	if expect.jwksURI == "" {
		return UpstreamIdentity{}, errors.New("issuer advertises no jwks_uri")
	}
	algorithms, err := acceptedIDTokenAlgorithms(expect.signingAlgs)
	if err != nil {
		return UpstreamIdentity{}, err
	}
	var claims jwt.Claims
	var all map[string]json.RawMessage
	if _, err := verifyIssuerSignedJWT(ctx, v.keys, expect.jwksURI, conv.Default(expect.fetchScope, expect.issuer), rawIDToken, algorithms, &claims, &all); err != nil {
		return UpstreamIdentity{}, err
	}
	if claims.Expiry == nil {
		return UpstreamIdentity{}, errors.New("id token has no exp")
	}
	// Compared by hand so a trailing-slash difference is tolerated like elsewhere in the package.
	if !issuerURLsEqual(claims.Issuer, expect.issuer) {
		return UpstreamIdentity{}, fmt.Errorf("id token issuer %q is not the grant's issuer", truncateForMessage(claims.Issuer))
	}
	now := time.Now()
	if err := claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      "",
		Subject:     "",
		AnyAudience: jwt.Audience{expect.clientID},
		ID:          "",
		Time:        now,
	}, idTokenMaxSkew); err != nil {
		return UpstreamIdentity{}, fmt.Errorf("validate id token claims: %w", err)
	}
	// §3.1.3.7 steps 4 and 5: an azp claim must name this client, and one
	// is required when the token names several audiences.
	if azp := claimString(all, "azp"); (azp != "" || len(claims.Audience) > 1) && azp != expect.clientID {
		return UpstreamIdentity{}, errors.New("id token authorized party is not this client")
	}
	if claims.Subject == "" {
		return UpstreamIdentity{}, errors.New("id token has no sub")
	}
	if expect.subject != "" && claims.Subject != expect.subject {
		return UpstreamIdentity{}, errIDTokenSubjectMismatch
	}
	// An empty expectation is a state minted before the nonce existed; bounded to the login-state TTL after rollout.
	if expect.nonce != "" && claimString(all, "nonce") != expect.nonce {
		return UpstreamIdentity{}, errors.New("id token nonce does not match the authorize request")
	}

	identity := UpstreamIdentity{
		Subject:     claims.Subject,
		Email:       claimString(all, "email"),
		DisplayName: displayNameFromClaims(all),
		Source:      IdentitySourceIDToken,
		Claims:      all,
	}
	return identity, nil
}

// verifyIssuerSignedJWT checks one signature on raw against the issuer's
// published key set, charging key fetches to fetchScope, and decodes the
// claims into dest. The header comes back for checks the caller owns.
func verifyIssuerSignedJWT(ctx context.Context, keys *jwks.KeyResolver, jwksURI, fetchScope, raw string, algorithms []jose.SignatureAlgorithm, dest ...any) (jose.Header, error) {
	var none jose.Header
	source, err := jwks.NewRemoteSource(jwksURI)
	if err != nil {
		return none, fmt.Errorf("issuer jwks_uri: %w", err)
	}
	token, err := jwt.ParseSigned(raw, algorithms)
	if err != nil {
		return none, fmt.Errorf("parse jwt: %w", err)
	}
	if len(token.Headers) != 1 {
		return none, errors.New("jwt must carry exactly one signature")
	}
	header := token.Headers[0]
	key, err := keys.VerificationKeyForAlgorithm(ctx, source.WithFetchScope(fetchScope), header.KeyID, jose.SignatureAlgorithm(header.Algorithm))
	if err != nil {
		return none, fmt.Errorf("resolve jwt signing key: %w", err)
	}
	if err := token.Claims(key, dest...); err != nil {
		return none, fmt.Errorf("verify jwt signature: %w", err)
	}
	return header, nil
}

// displayNameFromClaims picks the best human-readable name the standard
// OpenID Connect claims offer.
func displayNameFromClaims(claims map[string]json.RawMessage) string {
	if name := claimString(claims, "name"); name != "" {
		return name
	}
	given, family := claimString(claims, "given_name"), claimString(claims, "family_name")
	if full := strings.TrimSpace(given + " " + family); full != "" {
		return full
	}
	return claimString(claims, "preferred_username")
}

// claimString reads a string claim, tolerating absence and other types.
func claimString(claims map[string]json.RawMessage, name string) string {
	var s string
	if raw, ok := claims[name]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// enrichmentDocument is the enrichment column: each interface's answer under
// its own key, the non-standard token response members, and the last outcome
// of every interface that was asked.
type enrichmentDocument struct {
	IDToken       map[string]json.RawMessage `json:"id_token,omitempty"`
	TokenResponse map[string]json.RawMessage `json:"token_response,omitempty"`
	Userinfo      map[string]json.RawMessage `json:"userinfo,omitempty"`
	Introspection map[string]json.RawMessage `json:"introspection,omitempty"`
	Interfaces    map[string]interfaceRecord `json:"interfaces,omitempty"`
}

// buildEnrichment serializes the enrichment column at exchange or refresh;
// nil when there is nothing to keep.
func buildEnrichment(tok tokenResponse, identity *UpstreamIdentity, interfaces map[string]interfaceRecord) ([]byte, error) {
	doc := enrichmentDocument{IDToken: nil, TokenResponse: tok.extras(), Userinfo: nil, Introspection: nil, Interfaces: interfaces}
	if identity != nil {
		claims := retainedClaims(identity.Claims)
		// email_verified describes the email beside it; without one it is meaningless.
		if claimString(claims, "email") == "" {
			delete(claims, "email_verified")
			if len(claims) == 0 {
				claims = nil
			}
		}
		switch identity.Source {
		case IdentitySourceIDToken:
			doc.IDToken = claims
		case IdentitySourceUserinfo:
			doc.Userinfo = claims
		}
	}
	if doc.IDToken == nil && doc.TokenResponse == nil && doc.Userinfo == nil && len(doc.Interfaces) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal enrichment document: %w", err)
	}
	if len(raw) > maxEnrichmentBytes {
		return nil, errEnrichmentTooLarge
	}
	return raw, nil
}

// tokenResponseUnchanged reports whether every member of extras already reads the same in the stored document.
func tokenResponseUnchanged(stored []byte, extras map[string]json.RawMessage) bool {
	if len(stored) == 0 {
		return len(extras) == 0
	}
	var doc enrichmentDocument
	if err := json.Unmarshal(stored, &doc); err != nil {
		return false
	}
	for name, raw := range extras {
		have, ok := doc.TokenResponse[name]
		if !ok || !jsonValuesEqual(have, raw) {
			return false
		}
	}
	return true
}

// jsonValuesEqual compares two JSON values structurally, since jsonb reformats what it stores.
func jsonValuesEqual(a, b json.RawMessage) bool {
	var x, y any
	da := json.NewDecoder(bytes.NewReader(a))
	da.UseNumber()
	db := json.NewDecoder(bytes.NewReader(b))
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// retainedClaims strips credential-shaped members at every nesting level; nil when nothing survives.
func retainedClaims(claims map[string]json.RawMessage) map[string]json.RawMessage {
	members := make(map[string]any, len(claims))
	for name, raw := range claims {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			continue
		}
		members[name] = value
	}
	stripCredentialMembers(members)
	if len(members) == 0 {
		return nil
	}
	kept := make(map[string]json.RawMessage, len(members))
	for name, value := range members {
		raw, err := json.Marshal(value)
		if err != nil {
			continue
		}
		kept[name] = raw
	}
	return kept
}

// logIdentityFailure logs at warn with capped error text; the token value is never logged.
func logIdentityFailure(ctx context.Context, logger *slog.Logger, msg string, err error, attrs ...slog.Attr) {
	capped := errors.New(truncateForMessage(err.Error()))
	logger.LogAttrs(ctx, slog.LevelWarn, msg, append([]slog.Attr{attr.SlogError(capped)}, attrs...)...)
}
