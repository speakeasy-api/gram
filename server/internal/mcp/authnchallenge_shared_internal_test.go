package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

func TestRedactResourceForLog(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		resource string
		want     string
	}{
		{name: "canonical", resource: "https://gram.example/mcp/billing", want: "https://gram.example/mcp/billing"},
		{name: "userinfo", resource: "https://user:secret@gram.example/mcp/billing", want: "https://gram.example/mcp/billing"},
		{name: "query and fragment", resource: "https://gram.example/mcp/billing?token=secret#frag", want: "https://gram.example/mcp/billing"},
		{name: "unparseable", resource: "https://gram.example/%zz", want: unparseableResourceLogValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, redactResourceForLog(tc.resource))
		})
	}
}

// A shared authorization server on the authentication host on a surface that
// cannot serve the workload grant advertises an empty grant list rather than
// omitting it, which RFC 8414 would read as the authorization-code and
// implicit grants.
func TestWorkloadAuthorizationServerMetadata_NoGrantWhenWorkloadGrantNotServed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		served bool
		want   string
	}{
		{name: "served", served: true, want: `["` + oauthwire.GrantTypeJWTBearer + `"]`},
		{name: "not served", served: false, want: `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(workloadAuthorizationServerMetadata(AuthorizationServerURLs{Issuer: "https://id.example.com/oauth/usi/x", Authorize: "", Token: "https://id.example.com/oauth/usi/x/token", Register: "", Revoke: "https://id.example.com/oauth/usi/x/revoke"}, tc.served))
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			require.JSONEq(t, tc.want, string(fields["grant_types_supported"]))
			require.JSONEq(t, `["`+oauthwire.AuthMethodNone+`"]`, string(fields["token_endpoint_auth_methods_supported"]))
			require.NotContains(t, fields, "token_endpoint_auth_signing_alg_values_supported")
		})
	}
}
