package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type enrolledDeviceManagementStub struct {
	DeviceEnrollmentTokenManagement
	list func(context.Context, *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error)
}

func (s enrolledDeviceManagementStub) List(ctx context.Context, payload *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
	return s.list(ctx, payload)
}

func TestEnrolledDeviceToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerEnrolledDeviceTools(live, &DeviceEnrollmentTokenService{})
	registerEnrolledDeviceTools(unavailable, nil)
	a, b := descriptorByName(t, live, "list_enrolled_devices"), descriptorByName(t, unavailable, "list_enrolled_devices")
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.output, b.output)
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
	require.Equal(t, []authz.Scope{authz.ScopeWorkloadRead}, a.Meta.DiscoveryScopes)
	require.True(t, a.Annotations.ReadOnlyHint)
	require.Empty(t, live.For(AudienceAssistant))
	require.Contains(t, a.Description, "expected agent assignment")
	_, err := b.Invoke(contextWithPrincipal(t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"}), []byte(`{"project_id":"`+uuid.NewString()+`","agent_id":"`+uuid.NewString()+`"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, unavailableCode)
}

func TestEnrolledDevicesScopedProjectionAndPagination(t *testing.T) {
	t.Parallel()
	p := Principal{OrganizationID: "test-org", UserID: "test-user"}
	projectID, agentID := uuid.New(), uuid.New()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: p.OrganizationID, UserID: p.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, p.UserID))
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	slices.Sort(ids)
	rows := []*types.WorkloadAdmission{
		{ID: ids[2], AgentID: agentID.String(), IssuerName: "Speakeasy devices", Subject: "device:private-third", Name: "Example three", Tags: []string{"private-tag"}},
		{ID: ids[0], AgentID: agentID.String(), IssuerName: "Speakeasy devices", Subject: "device:private-first", Name: "Example one"},
		{ID: ids[1], AgentID: agentID.String(), IssuerName: "Speakeasy devices", Subject: "device:private-second", Name: "Example two"},
		{ID: uuid.NewString(), AgentID: uuid.NewString(), IssuerName: "Speakeasy devices", Subject: "device:private-other-agent"},
		{ID: uuid.NewString(), AgentID: agentID.String(), IssuerName: "External platform", Subject: "device:private-other-issuer"},
		{ID: uuid.NewString(), AgentID: agentID.String(), IssuerName: "Speakeasy devices", Subject: "private-other-subject"},
		{ID: uuid.NewString(), AgentID: agentID.String(), IssuerName: "Speakeasy devices", Subject: "device:private-project-tier", ProjectID: projectID.String()},
	}
	calls := 0
	readError := false
	service := &DeviceEnrollmentTokenService{
		resolveProject: func(_ context.Context, organizationID string, input FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, p.OrganizationID, organizationID)
			require.Equal(t, projectID.String(), input.ProjectID)
			return ResolvedProject{ID: projectID, Slug: "explicit"}, nil
		},
		management: enrolledDeviceManagementStub{
			DeviceEnrollmentTokenManagement: nil,
			list: func(scoped context.Context, _ *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
				calls++
				ac, ok := contextvalues.GetAuthContext(scoped)
				require.True(t, ok)
				require.Equal(t, p.UserID, ac.UserID)
				require.Equal(t, projectID, *ac.ProjectID)
				if readError {
					return nil, oops.C(oops.CodeForbidden)
				}
				return &gen.WorkloadIdentityPolicy{Issuers: nil, Admissions: rows}, nil
			},
		},
	}
	input := ListEnrolledDevicesInput{ProjectID: projectID.String(), AgentID: strings.ToUpper(agentID.String()), Cursor: "", Limit: 2}
	first, err := service.listEnrolledDevices(ctx, p, input)
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.Equal(t, ids[0], first.Items[0].ID)
	require.Equal(t, ids[1], *first.NextCursor)
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	input.Cursor = strings.ToUpper(*first.NextCursor)
	second, err := service.listEnrolledDevices(ctx, p, input)
	require.NoError(t, err)
	require.Equal(t, ids[2], second.Items[0].ID)
	require.Nil(t, second.NextCursor)
	input.Limit = 101
	_, err = service.listEnrolledDevices(ctx, p, input)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	input.Limit = 2
	_, err = service.listEnrolledDevices(ctx, Principal{OrganizationID: "foreign", UserID: p.UserID}, input)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Nil(t, ac.ProjectID)
	_, err = service.listEnrolledDevices(contextvalues.WithLegacyAPIKeyAuthorization(ctx, ac), p, input)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	readError = true
	result, err := service.listEnrolledDevices(ctx, p, input)
	require.Error(t, err)
	require.Empty(t, result.Items)
}

func TestEnrolledDevicesRefusalDoesNotExposeBackendDetails(t *testing.T) {
	t.Parallel()
	for _, code := range []oops.Code{oops.CodeForbidden, oops.CodeInvalid, oops.CodeUnexpected} {
		t.Run(string(code), func(t *testing.T) {
			t.Parallel()
			result, ok := enrolledDeviceToolResult(oops.E(code, nil, "private backend details"))
			require.True(t, ok)
			require.True(t, result.IsError)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			require.NotContains(t, text.Text, "private backend details")
			require.NotContains(t, text.Text, "revocation")
		})
	}
}
