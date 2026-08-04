package oauth21

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
)

const (
	testAppClientID  = "requesting-app"
	testAppSecret    = "requesting-app-secret"
	testResourceSlug = "chat"
	testResourceID   = "https://mcp.chat.example/"
)

// mintFixture is a fully seeded, policy-satisfying mint setup. Denial tests
// start from it and break exactly one thing, so a failure names the rule that
// actually fired.
type mintFixture struct {
	*dbHandler
	app        repo.EmaApp
	user       repo.User
	resource   repo.EmaResource
	idToken    string
	audience   string
	assignment repo.EmaAppAssignment
}

func newMintFixture(t *testing.T) *mintFixture {
	t.Helper()

	h := newDBHandler(t)
	app := h.seedApp(t, testAppClientID, testAppSecret)
	user := h.seedUser(t, "mint@devidptest.local")
	resource := h.seedResource(t, testResourceSlug, testResourceID)
	assignment := h.seedAssignment(t, app, user, resource, "chat.read chat.history")

	return &mintFixture{
		dbHandler:  h,
		app:        app,
		user:       user,
		resource:   resource,
		idToken:    h.signIDToken(t, user),
		audience:   ema.ResourceASIssuer(testExternalURL, testResourceSlug),
		assignment: assignment,
	}
}

// form builds a mint request that satisfies every rule. Tests mutate the
// returned values to express the one case under test.
func (f *mintFixture) form() url.Values {
	v := url.Values{}
	v.Set("grant_type", ema.GrantTypeTokenExchange)
	v.Set("requested_token_type", ema.TokenTypeIDJAG)
	v.Set("audience", f.audience)
	v.Set("resource", testResourceID)
	v.Set("scope", "chat.read chat.history")
	v.Set("subject_token", f.idToken)
	v.Set("subject_token_type", ema.TokenTypeIDToken)
	v.Set("client_id", testAppClientID)
	v.Set("client_secret", testAppSecret)
	return v
}

func (f *mintFixture) mint(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return f.postForm(t, "/token", form)
}

// decodeError reads an OAuth error envelope off a response.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "decode error envelope")
	return body
}

// parseIDJAG verifies an ID-JAG against the handler's own key and returns its
// claims plus the raw header, so tests can assert on both.
func parseIDJAG(t *testing.T, h *dbHandler, raw string) (ema.Claims, map[string]any) {
	t.Helper()

	var claims ema.Claims
	token, err := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"})).ParseWithClaims(
		raw, &claims, func(*jwt.Token) (any, error) { return h.keystore.PublicKey(), nil },
	)
	require.NoError(t, err, "parse minted ID-JAG")
	require.True(t, token.Valid, "minted ID-JAG should verify")
	return claims, token.Header
}

func TestMintIDJAGIssuesAProfileConformantGrant(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	rec := f.mint(t, f.form())
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	// RFC 8693 §2.2.1 requires the response not be cached.
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, ema.TokenTypeIDJAG, body.IssuedTokenType)
	require.Equal(t, ema.TokenTypeNotApplicable, body.TokenType, "an ID-JAG is a grant, not a bearer credential")
	require.Equal(t, "chat.read chat.history", body.Scope)
	require.Positive(t, body.ExpiresIn)

	claims, header := parseIDJAG(t, f.dbHandler, body.AccessToken)

	// The typ header is what stops an ordinary id_token being replayed into
	// the jwt-bearer grant, so it is not optional.
	require.Equal(t, ema.JWTType, header["typ"])
	require.Equal(t, f.keystore.KID(), header["kid"])

	require.Equal(t, testExternalURL+Prefix, claims.Issuer)
	require.Equal(t, f.user.ID.String(), claims.Subject)
	require.Equal(t, f.user.Email, claims.Email)
	require.Equal(t, testAppClientID, claims.ClientID)
	require.Equal(t, "chat.read chat.history", claims.Scope)
	require.NotEmpty(t, claims.ID, "jti is required")

	// aud is the resource authorization server; resource is the protected
	// resource behind it. Conflating the two is the classic way to get this
	// profile wrong, so assert they are distinct and each correct.
	require.Equal(t, jwt.ClaimStrings{f.audience}, claims.Audience)
	require.Equal(t, testResourceID, claims.Resource)
	require.NotEqual(t, claims.Resource, claims.Audience[0])
}

func TestMintIDJAGRecordsTheGrantInTheLedger(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	rec := f.mint(t, f.form())
	require.Equal(t, http.StatusOK, rec.Code)

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	claims, _ := parseIDJAG(t, f.dbHandler, body.AccessToken)

	issued, err := f.queries.ListEmaIssuedJags(t.Context(), repo.ListEmaIssuedJagsParams{
		UserID:     uuid.NullUUID{UUID: f.user.ID, Valid: true},
		ResourceID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MaxRows:    10,
	})
	require.NoError(t, err)
	require.Len(t, issued, 1)
	require.Equal(t, claims.ID, issued[0].Jti)
	require.Equal(t, "chat.read chat.history", issued[0].Scope)
	require.Equal(t, f.app.ID, issued[0].AppID)
}

func TestMintIDJAGAcceptsARefreshTokenSubject(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("subject_token", f.seedRefreshToken(t, f.user))
	form.Set("subject_token_type", ema.TokenTypeRefreshToken)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	claims, _ := parseIDJAG(t, f.dbHandler, body.AccessToken)
	require.Equal(t, f.user.ID.String(), claims.Subject)
}

func TestMintIDJAGOmittedScopeGrantsTheWholeAssignment(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Del("scope")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "chat.read chat.history", body.Scope)
}

func TestMintIDJAGNarrowsScopeToTheAssignment(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("scope", "chat.read chat.write")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "chat.read", body.Scope, "chat.write is not assigned and must be dropped")
}

func TestMintIDJAGRejectsScopeDisjointFromTheAssignment(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("scope", "chat.write chat.admin")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_scope", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsUnassignedUser(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	require.NoError(t, f.queries.DeleteEmaAppAssignment(t.Context(), f.assignment.ID))

	rec := f.mint(t, f.form())
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "access_denied", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsDisabledApp(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	// Every narg left invalid so COALESCE keeps the existing value; only
	// `enabled` is rewritten.
	_, err := f.queries.UpdateEmaApp(t.Context(), repo.UpdateEmaAppParams{
		ID:           f.app.ID,
		ClientID:     sql.NullString{String: "", Valid: false},
		ClientSecret: sql.NullString{String: "", Valid: false},
		Name:         sql.NullString{String: "", Valid: false},
		Enabled:      false,
		Ts:           time.Now(),
	})
	require.NoError(t, err)

	rec := f.mint(t, f.form())
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "disabled")
}

func TestMintIDJAGRejectsUnknownApp(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("client_id", "never-registered")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "invalid_client", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsWrongClientSecret(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("client_secret", "wrong")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "invalid_client", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsUnknownAudience(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("audience", ema.ResourceASIssuer(testExternalURL, "not-registered"))

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_target", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsForeignAudience(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("audience", "https://someone-elses-idp.example/")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_target", decodeError(t, rec)["error"])
}

func TestMintIDJAGAcceptsAudienceWithTrailingSlash(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("audience", f.audience+"/")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestMintIDJAGRejectsResourceNotBehindTheAudience(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("resource", "https://mcp.other.example/")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_target", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsForeignSignedSubjectToken(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	// An id_token from a different dev-idp instance: same shape, different key.
	other := newDBHandler(t)
	otherUser := other.seedUser(t, "mint@devidptest.local")
	form := f.form()
	form.Set("subject_token", other.signIDToken(t, otherUser))

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeError(t, rec)
	require.Equal(t, "invalid_grant", body["error"])
	// Assert the signature check fired, not the user-lookup that follows it —
	// both answer invalid_grant, and only one of them is the point here.
	require.Contains(t, body["error_description"], "not a valid id_token")
}

func TestMintIDJAGRejectsUnsupportedRequestedTokenType(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request", decodeError(t, rec)["error"])
}

func TestMintIDJAGRejectsUnsupportedSubjectTokenType(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:saml2")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request", decodeError(t, rec)["error"])
}

func TestMintIDJAGRequiresAudience(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	form := f.form()
	form.Del("audience")

	rec := f.mint(t, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request", decodeError(t, rec)["error"])
}

func TestASMetadataAdvertisesIDJAGMinting(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	rec := httptest.NewRecorder()
	h.Handler.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var doc struct {
		GrantTypesSupported []string `json:"grant_types_supported"`
		RequestedTokenTypes []string `json:"identity_chaining_requested_token_types_supported"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Contains(t, doc.GrantTypesSupported, ema.GrantTypeTokenExchange)
	require.Contains(t, doc.RequestedTokenTypes, ema.TokenTypeIDJAG)
}
