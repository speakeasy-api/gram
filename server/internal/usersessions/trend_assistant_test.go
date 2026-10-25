package usersessions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adapter "github.com/speakeasy-api/gram/server/internal/assistant_platform_mcp_adapter"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// liveOrgAdminStub stands in for the assistant surface's live
// organization-admin check, which the adapter makes before every call.
type liveOrgAdminStub struct{ refusal error }

func (liveOrgAdminStub) PrepareExternalContext(ctx context.Context, _ platformmcp.Principal) (context.Context, error) {
	return ctx, nil
}

func (liveOrgAdminStub) AuthorizeExternalCall(context.Context, platformmcp.Principal, platformmcp.ExternalAuthorization) error {
	return nil
}

func (liveOrgAdminStub) RequireLiveMembership(context.Context, platformmcp.Principal) error {
	return nil
}

func (s liveOrgAdminStub) RequireLiveOrgAdmin(context.Context, platformmcp.Principal) error {
	return s.refusal
}

// The managed assistant counts a gateway's connections through the real
// adapter, catalogue and session service: in its own project whatever the
// model names, with project:read and mcp:read on the gateway, and after the
// adapter's live organization-admin check.
func TestAssistantGetsGatewayConnectionTrend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := seedIssuer(t, ctx, ti, "trend-assistant")
	gateway := seedGateway(t, ctx, ti, "Assistant trend gateway", uuid.NullUUID{UUID: issuer, Valid: true})
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	seedTrendRow(t, ctx, ti, issuer, trendRow{identity: gatewayIdentity(gateway), selection: nil, refreshable: true, credential: none, selfRoot: false, created: at(1, 0), deleted: new(at(2, 0)), deadline: at(240, 0), expires: at(2, 0)})

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := platformmcp.NewPostgresReader(logger, ti.conn).WithAuthorization(engine)
	reader.WithGatewayConnections(platformmcp.NewGatewayConnectionsService(ti.service, reader))
	runtime := platformmcp.NewRuntime(logger, nil, nil, nil, "", "trend-assistant-key", reader, nil, nil, nil, nil)
	admin := &liveOrgAdminStub{refusal: nil}
	var tool *adapter.Tool
	for _, composed := range adapter.Tools(runtime.AssistantTools(), admin) {
		if composed.Descriptor().Name == "get_gateway_connection_trend" {
			tool = &composed
		}
	}
	require.NotNil(t, tool, "the trend is composed for the assistant")
	var advertised struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.Descriptor().InputSchema, &advertised))
	require.NotContains(t, advertised.Properties, "project_id", "the assistant's project is injected")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project := *authCtx.ProjectID
	projectRead := authz.NewGrant(authz.ScopeProjectRead, project.String())
	gatewayRead := authz.NewGrant(authz.ScopeMCPRead, gateway.ID.String())
	call := func(grants ...authz.Grant) (string, error) {
		t.Helper()
		// An assistant turn: no dashboard session, the assistant's own
		// project, and its grants enforced.
		turnAuth := *authCtx
		turnAuth.SessionID = nil
		turn := contextvalues.SetAssistantPrincipal(t.Context(), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
		turn = contextvalues.SetAuthContext(turn, &turnAuth)
		arguments := `{"project_id":"` + uuid.NewString() + `","gateway_id":"` + gateway.ID.String() + `","from":"` + at(0, 0).Format(time.RFC3339) + `","to":"` + at(3, 0).Format(time.RFC3339) + `","bucket":"hour"}`
		var out bytes.Buffer
		err := tool.Call(authz.GrantsToContext(turn, grants), toolconfig.ToolCallEnv{}, strings.NewReader(arguments), &out)
		return out.String(), err
	}

	raw, err := call(projectRead, gatewayRead)
	require.NoError(t, err)
	var output platformmcp.GatewayConnectionTrendOutput
	require.NoError(t, json.Unmarshal([]byte(raw), &output), raw)
	require.Equal(t, project.String(), output.ProjectID, "the assistant's own project wins over the model's")
	require.Equal(t, []int64{0, 1, 1, 0}, []int64{output.ActiveAtStart, output.Created, output.Revoked, output.ActiveAtEnd})
	require.Len(t, output.Buckets, 3)

	raw, err = call(projectRead)
	require.NoError(t, err, "a refusal is the tool's answer")
	require.Contains(t, raw, "not_found", "a gateway the turn may not read is not found")
	_, err = call(gatewayRead)
	require.ErrorIs(t, err, platformmcp.ErrForbidden, "no project:read")

	admin.refusal = platformmcp.ErrForbidden
	raw, err = call(projectRead, gatewayRead)
	require.ErrorIs(t, err, platformmcp.ErrForbidden, "the adapter's live organization-admin check refuses first")
	require.Empty(t, raw)
}
