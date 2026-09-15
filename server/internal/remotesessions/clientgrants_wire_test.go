package remotesessions_test

import (
	"encoding/json"
	"testing"

	platform "github.com/speakeasy-api/gram/server/gen/http/admin_remote_sessions/server"
	organization "github.com/speakeasy-api/gram/server/gen/http/organization_remote_session_clients/server"
	project "github.com/speakeasy-api/gram/server/gen/http/remote_session_clients/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/stretchr/testify/require"
)

// Exercise generated transport conversions, not just the model view: omitempty
// would collapse unknown and explicitly empty grants at the HTTP boundary.
func TestClientGrantTypesWireDistinguishesUnknownEmptyAndPopulated(t *testing.T) {
	t.Parallel()
	encoders := map[string]func(*types.RemoteSessionClient) any{
		"project/create":      func(r *types.RemoteSessionClient) any { return project.NewCreateRemoteSessionClientResponseBody(r) },
		"project/read":        func(r *types.RemoteSessionClient) any { return project.NewGetRemoteSessionClientResponseBody(r) },
		"project/update":      func(r *types.RemoteSessionClient) any { return project.NewUpdateRemoteSessionClientResponseBody(r) },
		"project/cimd":        func(r *types.RemoteSessionClient) any { return project.NewCreateCimdResponseBody(r) },
		"organization/create": func(r *types.RemoteSessionClient) any { return organization.NewCreateClientResponseBody(r) },
		"organization/read":   func(r *types.RemoteSessionClient) any { return organization.NewGetClientResponseBody(r) },
		"organization/update": func(r *types.RemoteSessionClient) any { return organization.NewUpdateClientResponseBody(r) },
		"platform/create":     func(r *types.RemoteSessionClient) any { return platform.NewCreateGlobalClientResponseBody(r) },
		"platform/read":       func(r *types.RemoteSessionClient) any { return platform.NewGetGlobalClientResponseBody(r) },
		"platform/update":     func(r *types.RemoteSessionClient) any { return platform.NewUpdateGlobalClientResponseBody(r) },
	}
	for name, encode := range encoders {
		for _, tc := range []struct {
			name   string
			grants []string
			want   string
		}{
			{"unknown", nil, "null"},
			{"empty", []string{}, "[]"},
			{"populated", []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"}, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				wire, err := json.Marshal(encode(&types.RemoteSessionClient{GrantTypes: tc.grants}))
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire, &fields))
				grantJSON, present := fields["grant_types"]
				require.True(t, present, "grant_types must always appear, including unknown and empty")
				require.JSONEq(t, tc.want, string(grantJSON))
			})
		}
	}
}
