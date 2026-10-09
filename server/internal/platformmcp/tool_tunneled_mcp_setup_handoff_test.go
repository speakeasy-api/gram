package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The stub must advertise the live contract: the same input and output schema,
// annotations, audiences, authorization and project scope, so the tool never
// changes shape as the dashboard origin or budget is composed.
func TestTunneledSetupHandoffStubMatchesLiveContract(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(live, &TunneledMCPSetupHandoffService{})
	stub := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(stub, nil)

	liveDescriptor := descriptorByName(t, live, getTunneledMCPSetupHandoffToolName)
	stubDescriptor := descriptorByName(t, stub, getTunneledMCPSetupHandoffToolName)
	require.JSONEq(t, string(liveDescriptor.InputSchema), string(stubDescriptor.InputSchema))
	require.Equal(t, liveDescriptor.output, stubDescriptor.output)
	require.Equal(t, liveDescriptor.Annotations, stubDescriptor.Annotations)
	require.Equal(t, liveDescriptor.Meta, stubDescriptor.Meta)
	require.Equal(t, liveDescriptor.Description, stubDescriptor.Description)

	require.Equal(t, ExternalAuthorizationOrgAdmin, liveDescriptor.Meta.Authorization)
	require.ElementsMatch(t, bothAudiences, liveDescriptor.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, liveDescriptor.Meta.ProjectScope)
	require.True(t, liveDescriptor.Annotations.ReadOnlyHint)
	require.NotNil(t, liveDescriptor.Annotations.OpenWorldHint)
	require.False(t, *liveDescriptor.Annotations.OpenWorldHint)

	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(liveDescriptor.InputSchema, &schema))
	require.Equal(t, []string{"project_id"}, schema.Required)
	require.Contains(t, schema.Properties, "mcp_id")
}

func TestTunneledSetupHandoffStubRefusesAsUnavailable(t *testing.T) {
	t.Parallel()

	stub := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerTunneledMCPSetupHandoffTool(stub, nil)

	out, err := descriptorByName(t, stub, getTunneledMCPSetupHandoffToolName).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"project_id":"00000000-0000-0000-0000-000000000001"}`),
	)
	require.Nil(t, out)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
	require.NotContains(t, refusal.Payload, "setup_url")
}

func TestTunneledSetupHandoffComposesOnlyWithSafeDependencies(t *testing.T) {
	t.Parallel()

	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	budget := allowBudget()
	compose := func(withAuthorization bool, dashboardURL string, budget OperationBudget) *TunneledMCPSetupHandoffService {
		reader := NewPostgresReader(testenv.NewLogger(t), new(pgxpool.Pool))
		if withAuthorization {
			reader.WithAuthorization(engine)
		}
		var origin *url.URL
		if dashboardURL != "" {
			origin = mustParseURL(t, dashboardURL)
		}
		return reader.WithTunneledMCPSetupHandoff(origin, budget).tunneledSetup
	}

	require.Nil(t, compose(true, "", budget), "no dashboard origin")
	require.Nil(t, compose(true, "http://dashboard.example.test", budget), "a plain-http origin")
	require.Nil(t, compose(true, "https://user:pass@dashboard.example.test", budget), "an origin carrying credentials")
	require.Nil(t, compose(true, "https://dashboard.example.test", OperationBudget{}), "no handoff budget")
	require.Nil(t, compose(false, "https://dashboard.example.test", budget), "no authorization engine")

	service := compose(true, "https://dashboard.example.test/base?x=1#frag", budget)
	require.NotNil(t, service)
	require.Equal(t, "https://dashboard.example.test/base", service.dashboardURL.String())
}
