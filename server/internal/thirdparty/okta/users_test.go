package okta

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClient_ListUsersBoundedDirectoryRead(t *testing.T) {
	t.Parallel()
	tc := newDefaultTestClient(t)
	tc.stub.setGrantedScopes([]string{"okta.users.read"})
	// Mint a real users-only token before replacing management transport.
	verification, err := tc.client.VerifyScopes(t.Context(), []string{"okta.users.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"okta.users.read"}, verification.Granted)
	calls := 0
	transport := tc.client.httpClient.Transport
	tc.client.httpClient.Transport = reviewTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return transport.RoundTrip(r)
		}
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/users", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		response := reviewResponse(http.StatusOK, `[{"id":"00u1"}]`)
		response.Header.Set("Link", "<"+r.URL.String()+"&after=00u1>; rel=\"next\"")
		return response, nil
	})
	users, err := tc.client.ListUsers(t.Context(), ListUsersRequest{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []User{{ID: "00u1"}}, users)
	require.Equal(t, 1, calls)
}

func TestFake_SetMethodErrorZeroValue(t *testing.T) {
	t.Parallel()
	var fake Fake
	want := errors.New("denied")
	fake.SetMethodError("ListUsers", nil)
	fake.SetMethodError("ListUsers", want)
	_, err := fake.ListUsers(t.Context(), ListUsersRequest{Limit: 1})
	require.ErrorIs(t, err, want)
	fake.SetMethodError("ListUsers", nil)
	_, err = fake.ListUsers(t.Context(), ListUsersRequest{Limit: 1})
	require.NoError(t, err)
}

func TestFake_ListUsersLimitAndIsolation(t *testing.T) {
	t.Parallel()
	fake := NewFake(Fixtures{Users: []User{{ID: "00u1"}, {ID: "00u2"}}})
	users, err := fake.ListUsers(t.Context(), ListUsersRequest{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []User{{ID: "00u1"}}, users)
	users[0].ID = "changed"
	users, err = fake.ListUsers(t.Context(), ListUsersRequest{})
	require.NoError(t, err)
	require.Equal(t, []User{{ID: "00u1"}, {ID: "00u2"}}, users)
}
