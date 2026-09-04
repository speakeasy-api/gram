package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// idTokenVerifyBudget bounds one verification, key fetch included. Identity
// capture is best effort: past the budget the session is stored without it
// rather than holding a login callback or a refresh on the issuer's key set.
// Refresh-path verification runs outside the single-flight lease, so the
// budget never counts against it.
const idTokenVerifyBudget = 5 * time.Second

// errIDTokenSubjectMismatch reports an ID token for someone other than the
// identity the session carries.
var errIDTokenSubjectMismatch = errors.New("id token subject differs from the identity the session carries")

var (
	// IDTokenKeyRefreshRate bounds how often one issuer's key set is re-fetched
	// because an ID token named an unknown kid.
	IDTokenKeyRefreshRate = ratelimit.PerMinute(10)

	// IDTokenKeyFetchRate bounds every upstream key set consult an issuer
	// causes, cold or forced.
	IDTokenKeyFetchRate = ratelimit.PerMinute(30)
)

// UpstreamIdentity is who an upstream grant belongs to at the provider, as a
// session-enrichment interface reported it. Fields the interface did not
// state are zero.
type UpstreamIdentity struct {
	// Subject is the provider's stable identifier for the user.
	Subject string

	// Email is the user's email at the provider.
	Email string

	// EmailVerified is whether the provider vouches for Email; nil when the
	// provider did not say.
	EmailVerified *bool

	// DisplayName is the best human-readable name the interface offered:
	// name, then given and family name, then a preferred username.
	DisplayName string

	// PictureURL is the user's avatar at the provider.
	PictureURL string

	// SessionID is the provider's own session identifier (OpenID Connect
	// sid), which back-channel logout names when it revokes.
	SessionID string

	// AuthTime is when the user last authenticated at the provider.
	AuthTime *time.Time

	// Source is the identity_source value naming the interface.
	Source string

	// VerifiedAt is when Gram verified the interface's answer.
	VerifiedAt time.Time

	// Claims is every claim the interface returned; the enrichment column
	// keeps them minus credential-shaped members.
	Claims map[string]json.RawMessage
}

// identityColumns is an identity projected onto the nullable remote_sessions
// identity columns: NULL wherever the identity is nil or the field is empty.
type identityColumns struct {
	Subject       pgtype.Text
	Email         pgtype.Text
	EmailVerified pgtype.Bool
	DisplayName   pgtype.Text
	PictureURL    pgtype.Text
	SessionID     pgtype.Text
	AuthTime      pgtype.Timestamptz
	Source        pgtype.Text
	VerifiedAt    pgtype.Timestamptz
}

// columns projects the identity onto its columns; a nil identity is all NULL.
func (i *UpstreamIdentity) columns() identityColumns {
	if i == nil {
		return identityColumns{
			Subject:       pgtype.Text{String: "", Valid: false},
			Email:         pgtype.Text{String: "", Valid: false},
			EmailVerified: pgtype.Bool{Bool: false, Valid: false},
			DisplayName:   pgtype.Text{String: "", Valid: false},
			PictureURL:    pgtype.Text{String: "", Valid: false},
			SessionID:     pgtype.Text{String: "", Valid: false},
			AuthTime:      conv.PtrToPGTimestamptz(nil),
			Source:        pgtype.Text{String: "", Valid: false},
			VerifiedAt:    conv.PtrToPGTimestamptz(nil),
		}
	}
	return identityColumns{
		Subject:       conv.ToPGTextEmpty(i.Subject),
		Email:         conv.ToPGTextEmpty(i.Email),
		EmailVerified: conv.PtrToPGBool(i.EmailVerified),
		DisplayName:   conv.ToPGTextEmpty(i.DisplayName),
		PictureURL:    conv.ToPGTextEmpty(i.PictureURL),
		SessionID:     conv.ToPGTextEmpty(i.SessionID),
		AuthTime:      conv.PtrToPGTimestamptz(i.AuthTime),
		Source:        conv.ToPGTextEmpty(i.Source),
		VerifiedAt:    conv.ToPGTimestamptz(i.VerifiedAt),
	}
}

// IDTokenVerifier verifies OpenID Connect ID tokens returned alongside an
// upstream access token, against the issuer's published key set. Safe for
// concurrent use.
type IDTokenVerifier struct {
	keys *jwks.KeyResolver
}

// NewIDTokenVerifier binds the key resolver an ID token verification needs.
func NewIDTokenVerifier(keys *jwks.KeyResolver) *IDTokenVerifier {
	return &IDTokenVerifier{keys: keys}
}

// NewIDTokenKeyResolver assembles the key resolver an IDTokenVerifier uses:
// a memory cache over the shared resolver, with the refresh and fetch budgets
// charged to store.
func NewIDTokenKeyResolver(logger *slog.Logger, policy *guardian.Policy, meterProvider metric.MeterProvider, store ratelimit.Store) (*jwks.KeyResolver, error) {
	refreshLimiter := ratelimit.New(store, "remote_session_id_token_jwks_refresh", IDTokenKeyRefreshRate)
	fetchLimiter := ratelimit.New(store, "remote_session_id_token_jwks_fetch", IDTokenKeyFetchRate)
	keys, err := jwks.NewKeyResolver(jwks.NewResolver(policy, meterProvider, logger), jwks.NewMemoryCache(), refreshLimiter, fetchLimiter, logger)
	if err != nil {
		return nil, fmt.Errorf("new id token key resolver: %w", err)
	}
	return keys, nil
}

// idTokenExpectation is what a verified ID token must say: the issuer that
// minted the grant, the client it was minted for, the key set that signs
// for the issuer, and what the grant already established. Nonce is the
// value the authorize request carried and is empty on a refresh, where
// OpenID Connect Core §12.2 requires none; subject is the identity the
// session already holds, which a refresh must restate, and is empty at the
// exchange.
type idTokenExpectation struct {
	issuer   string
	clientID string
	jwksURI  string

	// fetchScope is the budget the key set fetch is charged to: the issuer
	// row's id, so one tenant's issuer cannot spend another's allowance by
	// claiming the same issuer URL.
	fetchScope string

	nonce   string
	subject string
}

// Verify checks an ID token per OpenID Connect Core §3.1.3.7 and returns the
// identity it asserts. The token itself is never retained: only its claims
// survive, and the caller must not log the raw value either.
func (v *IDTokenVerifier) Verify(ctx context.Context, rawIDToken string, expect idTokenExpectation) (UpstreamIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, idTokenVerifyBudget)
	defer cancel()

	if expect.jwksURI == "" {
		return UpstreamIdentity{}, errors.New("issuer advertises no jwks_uri")
	}
	source, err := jwks.NewRemoteSource(expect.jwksURI)
	if err != nil {
		return UpstreamIdentity{}, fmt.Errorf("issuer jwks_uri: %w", err)
	}
	token, err := jwt.ParseSigned(rawIDToken, jwks.AllowedSignatureAlgorithms())
	if err != nil {
		return UpstreamIdentity{}, fmt.Errorf("parse id token: %w", err)
	}
	if len(token.Headers) != 1 {
		return UpstreamIdentity{}, errors.New("id token must carry exactly one signature")
	}
	key, err := v.keys.VerificationKey(ctx, source.WithFetchScope(conv.Default(expect.fetchScope, expect.issuer)), token.Headers[0].KeyID)
	if err != nil {
		return UpstreamIdentity{}, fmt.Errorf("resolve id token signing key: %w", err)
	}

	var claims jwt.Claims
	var all map[string]json.RawMessage
	if err := token.Claims(key, &claims, &all); err != nil {
		return UpstreamIdentity{}, fmt.Errorf("verify id token signature: %w", err)
	}
	if claims.Expiry == nil {
		return UpstreamIdentity{}, errors.New("id token has no exp")
	}
	// The issuer is compared by hand rather than through jwt.Expected so a
	// trailing slash difference between the stored URL and the iss claim is
	// tolerated the way the rest of the package tolerates it.
	if !issuerURLsEqual(claims.Issuer, expect.issuer) {
		return UpstreamIdentity{}, fmt.Errorf("id token issuer %q is not the grant's issuer", claims.Issuer)
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
	if expect.nonce != "" && claimString(all, "nonce") != expect.nonce {
		return UpstreamIdentity{}, errors.New("id token nonce does not match the authorize request")
	}

	identity := UpstreamIdentity{
		Subject:       claims.Subject,
		Email:         claimString(all, "email"),
		EmailVerified: claimBool(all, "email_verified"),
		DisplayName:   displayNameFromClaims(all),
		PictureURL:    claimString(all, "picture"),
		SessionID:     claimString(all, "sid"),
		AuthTime:      claimTime(all, "auth_time"),
		Source:        IdentitySourceIDToken,
		VerifiedAt:    now,
		Claims:        all,
	}
	return identity, nil
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

// claimBool reads a boolean claim, accepting the string spellings some
// providers use; nil when absent or unreadable.
func claimBool(claims map[string]json.RawMessage, name string) *bool {
	raw, ok := claims[name]
	if !ok {
		return nil
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return &b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch strings.ToLower(s) {
		case "true":
			b = true
			return &b
		case "false":
			return &b
		}
	}
	return nil
}

// claimTime reads a NumericDate claim; nil when absent or unreadable.
func claimTime(claims map[string]json.RawMessage, name string) *time.Time {
	raw, ok := claims[name]
	if !ok {
		return nil
	}
	var seconds float64
	if json.Unmarshal(raw, &seconds) != nil || seconds <= 0 {
		return nil
	}
	t := time.Unix(int64(seconds), 0).UTC()
	return &t
}

// enrichmentDocument is what the enrichment column holds: the verified ID
// token claims and the non-standard members of the token response, each
// present only when there was something to keep.
type enrichmentDocument struct {
	IDToken       map[string]json.RawMessage `json:"id_token,omitempty"`
	TokenResponse map[string]json.RawMessage `json:"token_response,omitempty"`
}

// buildEnrichment serializes the enrichment column for a grant, or returns
// nil when neither the identity nor the token response added anything.
func buildEnrichment(tok tokenResponse, identity *UpstreamIdentity) []byte {
	doc := enrichmentDocument{IDToken: nil, TokenResponse: tok.extras()}
	if identity != nil && identity.Source == IdentitySourceIDToken {
		doc.IDToken = make(map[string]json.RawMessage, len(identity.Claims))
		for name, value := range identity.Claims {
			if !credentialMemberName(name) {
				doc.IDToken[name] = value
			}
		}
	}
	if doc.IDToken == nil && doc.TokenResponse == nil {
		return nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return raw
}

// logIdentityFailure records why an ID token was not accepted, with the
// attributes that tie the line to a grant. The token value is never part of
// the line.
func logIdentityFailure(ctx context.Context, logger *slog.Logger, err error, attrs ...slog.Attr) {
	args := make([]any, 0, len(attrs)+1)
	args = append(args, attr.SlogError(err))
	for _, a := range attrs {
		args = append(args, a)
	}
	logger.WarnContext(ctx, "upstream id token rejected; session stored without identity", args...)
}
