package devidptest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
)

func TestLaunch_ExposesOAuth21Metadata(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	require.NotEmpty(t, inst.Issuer)
	require.Equal(t, inst.Issuer+"/oauth2-1", inst.OAuth21URL)
	require.Empty(t, inst.WorkOSURL, "the WorkOS surface is opt-in")

	body := inst.OAuth21Metadata(t)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(body, &meta))
	require.Equal(t, inst.OAuth21URL, meta["issuer"])
	require.Equal(t, inst.OAuth21URL+"/token", meta["token_endpoint"])
	require.Equal(t, inst.OAuth21URL+"/register", meta["registration_endpoint"])
}

func TestLaunch_EnableWorkOS(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: true})

	require.Equal(t, inst.Issuer+"/workos", inst.WorkOSURL)

	cu, err := inst.Repo.GetCurrentUser(t.Context(), devidptest.WorkOSMode)
	require.NoError(t, err, "the workos currentUser slot should be seeded when enabled")
	require.Equal(t, inst.DefaultUser.ID.String(), cu.SubjectRef)
}

func TestCreateRefreshToken_OAuth21RefreshSucceeds(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	const seeded = "seeded-refresh-token"
	devidptest.CreateRefreshToken(t, t.Context(), inst.Repo, devidptest.RefreshTokenOpts{
		Token:  seeded,
		UserID: inst.DefaultUser.ID,
	})

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", seeded)
	form.Set("client_id", "ignored-by-devidp")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		inst.OAuth21URL+"/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"refresh against seeded token should succeed: %s", string(body))

	var tokResp map[string]any
	require.NoError(t, json.Unmarshal(body, &tokResp))
	require.NotEmpty(t, tokResp["access_token"])
	require.Equal(t, "Bearer", tokResp["token_type"])
}

func TestFactories_UserOrgMembership(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	user := devidptest.CreateUser(t, t.Context(), inst.Repo, devidptest.UserOpts{})
	require.NotEqual(t, uuid.Nil, user.User.ID)
	require.NotEmpty(t, user.User.Email)

	org := devidptest.CreateOrganization(t, t.Context(), inst.Repo, devidptest.OrganizationOpts{})
	require.NotEqual(t, uuid.Nil, org.Organization.ID)

	mem := devidptest.CreateMembership(t, t.Context(), inst.Repo, devidptest.MembershipOpts{
		UserID:         user.User.ID,
		OrganizationID: org.Organization.ID,
	})
	require.NotEqual(t, uuid.Nil, mem.Membership.ID)
	require.Equal(t, user.User.ID, mem.Membership.UserID)
	require.Equal(t, org.Organization.ID, mem.Membership.OrganizationID)
}

// publishedKIDs reads the kids the instance's OAuth 2.1 JWKS currently serves.
func publishedKIDs(t *testing.T, inst *devidptest.Instance) []string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, inst.OAuth21URL+"/.well-known/jwks.json", nil)
	require.NoError(t, err)
	resp, err := inst.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
		} `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))

	kids := make([]string, 0, len(doc.Keys))
	for _, key := range doc.Keys {
		kids = append(kids, key.Kid)
	}
	return kids
}

func TestLaunch_TLSServesEverythingOverHTTPS(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})

	require.True(t, strings.HasPrefix(inst.Issuer, "https://"), "issuer %q", inst.Issuer)
	require.True(t, strings.HasPrefix(inst.OAuth21URL, "https://"), "oauth2-1 issuer %q", inst.OAuth21URL)
	require.NotNil(t, inst.RootCAs())

	var meta map[string]any
	require.NoError(t, json.Unmarshal(inst.OAuth21Metadata(t), &meta))
	require.Equal(t, inst.OAuth21URL, meta["issuer"], "discovery must advertise the https issuer it is served from")
	require.Equal(t, inst.OAuth21URL+"/.well-known/jwks.json", meta["jwks_uri"])
}

func TestLaunch_PlainHTTPHasNoCertificateToTrust(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	require.True(t, strings.HasPrefix(inst.Issuer, "http://"), "issuer %q", inst.Issuer)
	require.Nil(t, inst.RootCAs())
}

// A rotation keeps the issuer URL.
func TestLaunch_RotateKeyRepublishesTheKeySet(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})
	issuer := inst.OAuth21URL
	retired := inst.KeyID()
	require.Equal(t, []string{retired}, publishedKIDs(t, inst))

	inst.RotateKey(t)

	require.NotEqual(t, retired, inst.KeyID())
	require.Equal(t, []string{inst.KeyID()}, publishedKIDs(t, inst))
	require.Equal(t, issuer, inst.OAuth21URL)
}

func TestLaunch_StopTakesTheServerOffTheNetwork(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})
	require.NotEmpty(t, publishedKIDs(t, inst), "the server must answer before it is stopped, or the test proves nothing")

	inst.Stop()
	inst.Stop()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, inst.OAuth21URL+"/.well-known/jwks.json", nil)
	require.NoError(t, err)
	resp, err := inst.Client().Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
}

func TestLaunch_RequestsCountsWhatReachesTheServer(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})
	before := inst.Requests()

	inst.OAuth21Metadata(t)
	publishedKIDs(t, inst)

	require.Equal(t, before+2, inst.Requests())
}

// An untrusting client fails the handshake without sending a request, which
// only the connection count sees.
func TestLaunch_ConnectionsCountsAttemptsThatNeverBecomeRequests(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})
	connectionsBefore := inst.Connections()
	requestsBefore := inst.Requests()

	untrusting := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(untrusting.CloseIdleConnections)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, inst.OAuth21URL+"/.well-known/jwks.json", nil)
	require.NoError(t, err)
	resp, err := untrusting.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "a client without the test certificate must fail the handshake")

	require.Equal(t, connectionsBefore+1, inst.Connections())
	require.Equal(t, requestsBefore, inst.Requests(), "the failed handshake never became a request")
}

func TestLaunch_SeedsDefaultUserAndCurrentUsers(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	require.NotEqual(t, uuid.Nil, inst.DefaultUser.ID)

	cu, err := inst.Repo.GetCurrentUser(t.Context(), devidptest.OAuth21Mode)
	require.NoError(t, err, "current_users for oauth2-1 should be seeded")
	require.Equal(t, inst.DefaultUser.ID.String(), cu.SubjectRef)
}
