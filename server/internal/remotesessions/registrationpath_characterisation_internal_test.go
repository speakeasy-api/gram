package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// refused is the expected outcome of a commit that refuses the provider's
// registration endpoint outright rather than asking for manual setup.
var refused = RegistrationPath("refused")

// TestDashboardRegistrationPathCharacterisation pins which registration path
// the dashboard's automatic setup takes for each kind of provider, in its
// default CIMD-preferring mode and in the mode that skips CIMD. The platform
// MCP attachment and inspector decisions for the same providers are pinned in
// the platformmcp package.
func TestDashboardRegistrationPathCharacterisation(t *testing.T) {
	t.Parallel()

	const (
		httpsEndpoint    = "https://idp.example.com/register"
		loopbackEndpoint = "http://127.0.0.1:8080/register"
	)
	tests := []struct {
		name         string
		endpoint     string
		methods      []string
		cimd         bool
		wantAuto     RegistrationPath
		wantSkipCIMD RegistrationPath
	}{
		{name: "dcr https with client_secret_basic", endpoint: httpsEndpoint, methods: []string{"client_secret_basic"}, wantAuto: RegistrationPathDCR, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr https with unenumerated methods", endpoint: httpsEndpoint, wantAuto: RegistrationPathDCR, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr https public clients only", endpoint: httpsEndpoint, methods: []string{"none"}, wantAuto: RegistrationPathDCR, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr loopback", endpoint: loopbackEndpoint, wantAuto: RegistrationPathDCR, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr endpoint padded with whitespace", endpoint: "  " + httpsEndpoint + " ", wantAuto: RegistrationPathDCR, wantSkipCIMD: RegistrationPathDCR},
		{name: "cimd only", methods: []string{"none"}, cimd: true, wantAuto: RegistrationPathCIMD, wantSkipCIMD: RegistrationPathManual},
		{name: "cimd refusing public clients", methods: []string{"client_secret_basic"}, cimd: true, wantAuto: RegistrationPathManual, wantSkipCIMD: RegistrationPathManual},
		{name: "dcr and cimd", endpoint: httpsEndpoint, methods: []string{"client_secret_basic", "none"}, cimd: true, wantAuto: RegistrationPathCIMD, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr public clients only and cimd", endpoint: httpsEndpoint, methods: []string{"none"}, cimd: true, wantAuto: RegistrationPathCIMD, wantSkipCIMD: RegistrationPathDCR},
		{name: "dcr loopback and cimd", endpoint: loopbackEndpoint, cimd: true, wantAuto: RegistrationPathCIMD, wantSkipCIMD: RegistrationPathDCR},
		{name: "plain http dcr endpoint", endpoint: "http://idp.example.com/register", wantAuto: refused, wantSkipCIMD: refused},
		{name: "plain http dcr endpoint and cimd", endpoint: "http://idp.example.com/register", cimd: true, wantAuto: RegistrationPathCIMD, wantSkipCIMD: refused},
		{name: "neither", methods: []string{"none"}, wantAuto: RegistrationPathManual, wantSkipCIMD: RegistrationPathManual},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := repo.RemoteSessionIssuer{
				RegistrationEndpoint:              pgtype.Text{String: test.endpoint, Valid: test.endpoint != ""},
				TokenEndpointAuthMethodsSupported: test.methods,
				ClientIDMetadataDocumentSupported: test.cimd,
			}
			require.Equal(t, test.wantAuto, dashboardRegistrationPath(t, "", provider), "automatic setup")
			require.Equal(t, test.wantAuto, dashboardRegistrationPath(t, "cimd", provider), "automatic setup preferring cimd")
			require.Equal(t, test.wantSkipCIMD, dashboardRegistrationPath(t, serverIdentityRegistrationMethodDCR, provider), "automatic setup skipping cimd")
		})
	}
}

// dashboardRegistrationPath is the path a commit for the stored provider takes
// under the policy the dashboard's automatic setup builds with
// registrationMethod.
func dashboardRegistrationPath(t *testing.T, registrationMethod string, provider repo.RemoteSessionIssuer) RegistrationPath {
	t.Helper()
	projectID := uuid.New()
	request := serverIdentityRequest{
		clientMode:          serverIdentityClientModeAuto,
		clientConfiguration: &gen.ServerIdentityClientConfiguration{},
		registrationMethod:  registrationMethod,
	}
	plan := request.plan(&contextvalues.AuthContext{ProjectID: &projectID}, mcpserversrepo.McpServer{})
	require.Equal(t, clientRegister, plan.Client.kind)
	commit := &IdentityCommit{plan: plan, provider: provider}
	path, err := commitRegistrationPath(plan.Client.policy, commit.capabilities())
	if err != nil {
		require.ErrorIs(t, err, ErrIdentityInvalid)
		return refused
	}
	return path
}
