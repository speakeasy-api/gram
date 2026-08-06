package oauth21

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
)

// cimdFixture is a mint setup whose app is identified by a metadata document
// URL and stores no JWKS of its own — its keys come from that document.
type cimdFixture struct {
	*mintFixture
	client *cimdClient
}

func newCIMDFixture(t *testing.T, inline bool, withKeys bool) *cimdFixture {
	t.Helper()

	h := newDBHandler(t)
	client := newCIMDClient(t, inline, withKeys)
	app := h.seedCIMDApp(t, client.clientID)
	user := h.seedUser(t, "cimd@devidptest.local")
	resource := h.seedResource(t, testResourceSlug, testResourceID)
	assignment := h.seedAssignment(t, app, user, resource, "chat.read chat.history")

	return &cimdFixture{
		mintFixture: &mintFixture{
			dbHandler:  h,
			app:        app,
			user:       user,
			resource:   resource,
			idToken:    h.signIDToken(t, user),
			audience:   ema.ResourceASIssuer(testExternalURL, testResourceSlug),
			assignment: assignment,
		},
		client: client,
	}
}

// form is the mint request as a CIMD client sends it: client_id is the
// document URL, and the credential is an assertion signed by the key that
// document publishes.
func (f *cimdFixture) form(t *testing.T) url.Values {
	t.Helper()
	v := f.mintFixture.form()
	v.Del("client_secret")
	v.Set("client_id", f.client.clientID)
	v.Set("client_assertion_type", ema.ClientAssertionTypeJWTBearer)
	v.Set(
		"client_assertion",
		signClientAssertion(
			t,
			f.defaultClientAssertion(f.client.clientID, f.client.key, f.client.kid),
		),
	)
	return v
}

// The document carries its keys inline, so one fetch resolves everything.
func TestMintAcceptsCIMDClientWithInlineJWKS(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, true, true)
	rec := f.mint(t, f.form(t))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body idJAGResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, ema.TokenTypeIDJAG, body.IssuedTokenType)
}

// The document points at a jwks_uri instead, which the resolver has to follow.
func TestMintAcceptsCIMDClientWithJwksURI(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, false, true)
	rec := f.mint(t, f.form(t))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

// A document declaring no key material describes a public client, which the
// draft allows — so the mint succeeds with no assertion at all.
func TestMintTreatsKeylessCIMDDocumentAsPublic(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, true, false)
	form := f.mintFixture.form()
	form.Del("client_secret")
	form.Set("client_id", f.client.clientID)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestMintRejectsCIMDAssertionSignedByTheWrongKey(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, true, true)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	form := f.mintFixture.form()
	form.Del("client_secret")
	form.Set("client_id", f.client.clientID)
	form.Set("client_assertion_type", ema.ClientAssertionTypeJWTBearer)
	form.Set(
		"client_assertion",
		signClientAssertion(
			t,
			f.defaultClientAssertion(f.client.clientID, other, f.client.kid),
		),
	)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "did not verify")
}

// A document whose keys exist must still be presented with an assertion; a
// CIMD client cannot silently downgrade itself to public.
func TestMintRejectsCIMDClientWithoutAnAssertion(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, true, true)
	form := f.mintFixture.form()
	form.Del("client_secret")
	form.Set("client_id", f.client.clientID)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "private_key_jwt")
}

// The draft requires the document's own client_id to equal the URL it came
// from; a mismatch means a document claiming an identity that is not its own.
func TestMintRejectsCIMDDocumentClaimingAnotherClientID(t *testing.T) {
	t.Parallel()

	h := newDBHandler(t)
	liar := newLyingCIMDClient(t)
	app := h.seedCIMDApp(t, liar.clientID)
	user := h.seedUser(t, "cimd@devidptest.local")
	resource := h.seedResource(t, testResourceSlug, testResourceID)
	assignment := h.seedAssignment(t, app, user, resource, "chat.read")

	f := &mintFixture{
		dbHandler:  h,
		app:        app,
		user:       user,
		resource:   resource,
		idToken:    h.signIDToken(t, user),
		audience:   ema.ResourceASIssuer(testExternalURL, testResourceSlug),
		assignment: assignment,
	}
	form := f.form()
	form.Del("client_secret")
	form.Set("client_id", liar.clientID)

	rec := f.mint(t, form)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, decodeError(t, rec)["error_description"], "does not match")
}

// Keys are consulted on every authenticated request, so the document has to be
// cached or a busy client would be a fetch per mint.
func TestCIMDDocumentIsCachedAcrossMints(t *testing.T) {
	t.Parallel()

	f := newCIMDFixture(t, true, true)
	for range 3 {
		rec := f.mint(t, f.form(t))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	}

	require.Equal(t, int32(1), f.client.fetches.Load(),
		"the metadata document should be fetched once and reused")
}
