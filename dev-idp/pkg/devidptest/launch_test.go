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
	require.Equal(t, inst.Issuer+"/oauth2", inst.OAuth20URL)
	require.Empty(t, inst.LocalSpeakeasyURL, "local-speakeasy is opt-in")

	body := inst.OAuth21Metadata(t)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(body, &meta))
	require.Equal(t, inst.OAuth21URL, meta["issuer"])
	require.Equal(t, inst.OAuth21URL+"/token", meta["token_endpoint"])
	require.Equal(t, inst.OAuth21URL+"/register", meta["registration_endpoint"])
}

func TestLaunch_ExposesOAuth20Metadata(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	body := inst.OAuth20Metadata(t)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(body, &meta))
	require.Equal(t, inst.OAuth20URL, meta["issuer"])
	require.Equal(t, inst.OAuth20URL+"/token", meta["token_endpoint"])
	require.NotContains(t, meta, "registration_endpoint",
		"oauth2 mode does not advertise DCR")
}

func TestLaunch_EnableLocalSpeakeasy(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{EnableLocalSpeakeasy: true})

	require.Equal(t, inst.Issuer+"/local-speakeasy", inst.LocalSpeakeasyURL)

	cu, err := inst.Repo.GetCurrentUser(t.Context(), devidptest.LocalSpeakeasyMode)
	require.NoError(t, err, "current_users for local-speakeasy should be seeded when enabled")
	require.Equal(t, inst.DefaultUser.ID.String(), cu.SubjectRef)
}

func TestCreateRefreshToken_OAuth21RefreshSucceeds(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	const seeded = "seeded-refresh-token"
	devidptest.CreateRefreshToken(t, t.Context(), inst.Repo, devidptest.RefreshTokenOpts{
		Token:  seeded,
		Mode:   devidptest.OAuth21Mode,
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

func TestCreateRefreshToken_OAuth20RefreshSucceeds(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	const seeded = "seeded-oauth2-refresh"
	devidptest.CreateRefreshToken(t, t.Context(), inst.Repo, devidptest.RefreshTokenOpts{
		Token:  seeded,
		Mode:   devidptest.OAuth20Mode,
		UserID: inst.DefaultUser.ID,
	})

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", seeded)
	form.Set("client_id", "ignored")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		inst.OAuth20URL+"/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
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

func TestLaunch_SeedsDefaultUserAndCurrentUsers(t *testing.T) {
	t.Parallel()

	inst := devidptest.Launch(t, devidptest.LaunchOpts{})

	require.NotEqual(t, uuid.Nil, inst.DefaultUser.ID)

	cu, err := inst.Repo.GetCurrentUser(t.Context(), devidptest.OAuth21Mode)
	require.NoError(t, err, "current_users for oauth2-1 should be seeded")
	require.Equal(t, inst.DefaultUser.ID.String(), cu.SubjectRef)

	cu, err = inst.Repo.GetCurrentUser(t.Context(), devidptest.OAuth20Mode)
	require.NoError(t, err, "current_users for oauth2 should be seeded")
	require.Equal(t, inst.DefaultUser.ID.String(), cu.SubjectRef)
}
