package assistant_platform_mcp_adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// liveOrgAdminStub stands in for the live organization-admin check the
// adapter makes before every call, answering with refusal; the project and
// gateway reads behind it are real.
type liveOrgAdminStub struct {
	refusal error
}

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

// The managed assistant reads a gateway's admission setting in its own project
// only, with project:read and mcp:read on the gateway and no log:read, after
// the adapter's live organization-admin check.
func TestAssistantReadsGatewayClientAdmissionInItsOwnProject(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := adapterInfra.CloneTestDatabase(t, "assistant_gateway_client_admission")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: organizationID, Name: "Assistant test organization", Slug: "org-" + uuid.NewString()[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}})
	require.NoError(t, err)
	projectIn := func(name string) uuid.UUID {
		row, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: name, Slug: name + "-" + uuid.NewString()[:8], OrganizationID: organizationID})
		require.NoError(t, err)
		return row.ID
	}
	own, other := projectIn("own"), projectIn("other")
	sessions := usersessionsrepo.New(conn)
	gatewayIn := func(projectID uuid.UUID, mode string) uuid.UUID {
		issuer, err := sessions.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: projectID, OrganizationID: pgtype.Text{String: organizationID, Valid: true}, Slug: "admission-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true}, AuthorizationServerMode: pgtype.Text{}, UseAuthenticationHost: pgtype.Bool{}})
		require.NoError(t, err)
		_, err = sessions.UpdateUserSessionIssuer(ctx, usersessionsrepo.UpdateUserSessionIssuerParams{Slug: pgtype.Text{}, AuthnChallengeMode: pgtype.Text{}, SessionDuration: pgtype.Interval{}, ClientIDMetadataAdmissionMode: pgtype.Text{String: mode, Valid: true}, ID: issuer.ID, ProjectID: projectID})
		require.NoError(t, err)
		_, err = sessions.CreateUserSessionIssuerCimdClient(ctx, usersessionsrepo.CreateUserSessionIssuerCimdClientParams{ClientIDMetadataUri: "https://client.example.test/metadata.json", UserSessionIssuerID: issuer.ID, ProjectID: projectID})
		require.NoError(t, err)
		row, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: organizationID, ProjectID: projectID, Name: "Gateway", UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, Visibility: visibility.Private, NetworkAccessMode: pgtype.Text{String: "", Valid: false}})
		require.NoError(t, err)
		return row.ID
	}
	restricted := gatewayIn(own, "presets")
	disabled := gatewayIn(own, "disabled")
	elsewhere := gatewayIn(other, "presets")

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := platformmcp.NewPostgresReader(logger, conn).WithAuthorization(engine)
	runtime := platformmcp.NewRuntime(logger, nil, nil, nil, "", "assistant-admission-test-key", reader, nil, nil, nil, nil)
	var tool *Tool
	admin := &liveOrgAdminStub{refusal: nil}
	for _, composed := range Tools(runtime.AssistantTools(), admin) {
		if composed.descriptor.Name == "get_gateway_client_admission" {
			tool = &composed
		}
	}
	require.NotNil(t, tool, "the tool is composed for the assistant")
	var advertised struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.Descriptor().InputSchema, &advertised))
	require.NotContains(t, advertised.Properties, "project_id", "the assistant's project is injected, never asked for")
	require.Contains(t, advertised.Properties, "gateway_id")

	userID := "user_" + uuid.NewString()
	grantsFor := func(gateways ...uuid.UUID) []authz.Grant {
		grants := []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, own.String())}
		for _, gateway := range gateways {
			grants = append(grants, authz.NewGrant(authz.ScopeMCPRead, gateway.String()))
		}
		return grants
	}
	call := func(grants []authz.Grant, arguments string) (string, error) {
		t.Helper()
		slug := "own"
		// A turn authenticates with an assistant token, which is what makes
		// its grants enforced on this session-less path.
		turn := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
		turn = contextvalues.SetAuthContext(turn, &contextvalues.AuthContext{ActiveOrganizationID: organizationID, UserID: userID, ProjectID: &own, ProjectSlug: &slug})
		var out bytes.Buffer
		err := tool.Call(authz.GrantsToContext(turn, grants), toolconfig.ToolCallEnv{}, strings.NewReader(arguments), &out)
		return out.String(), err
	}
	read := func(raw string) platformmcp.GatewayClientAdmissionOutput {
		t.Helper()
		var output platformmcp.GatewayClientAdmissionOutput
		require.NoError(t, json.Unmarshal([]byte(raw), &output), raw)
		return output
	}
	all := grantsFor(restricted, disabled, elsewhere)

	raw, err := call(all, `{"project_id":"`+other.String()+`","gateway_id":"`+restricted.String()+`"}`)
	require.NoError(t, err)
	output := read(raw)
	require.Equal(t, own.String(), output.ProjectID, "the assistant's own project wins over the model's")
	require.Equal(t, []any{true, "presets", false}, []any{output.SignIn, output.Mode, output.MoreCustomURLs})
	require.Equal(t, []string{"https://client.example.test/metadata.json"}, output.CustomClientURLs)

	raw, err = call(all, `{"gateway_id":"`+disabled.String()+`"}`)
	require.NoError(t, err)
	require.Equal(t, "disabled", read(raw).Mode)

	raw, err = call(all, `{"project_id":"`+other.String()+`","gateway_id":"`+elsewhere.String()+`"}`)
	require.NoError(t, err, "a refusal is the tool's answer")
	require.Contains(t, raw, "not_found", "another project's gateway, even when the model names that project")

	raw, err = call(grantsFor(disabled), `{"gateway_id":"`+restricted.String()+`"}`)
	require.NoError(t, err)
	require.Contains(t, raw, "not_found", "a gateway the turn may not read is hidden")

	_, err = call(grantsFor(restricted)[1:], `{"gateway_id":"`+restricted.String()+`"}`)
	require.ErrorIs(t, err, platformmcp.ErrForbidden, "no project:read")

	admin.refusal = platformmcp.ErrForbidden
	raw, err = call(all, `{"gateway_id":"`+restricted.String()+`"}`)
	require.ErrorIs(t, err, platformmcp.ErrForbidden, "the adapter's live organization-admin check refuses first")
	require.Empty(t, raw)
}
