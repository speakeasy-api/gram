package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
	"github.com/stretchr/testify/require"
)

type deviceEnrollmentManagementStub struct {
	list   func(context.Context, *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error)
	revoke func(context.Context, *gen.RevokeDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinTokenSummary, error)
}

func (s deviceEnrollmentManagementStub) ListDeviceJoinTokens(ctx context.Context, input *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error) {
	return s.list(ctx, input)
}
func (s deviceEnrollmentManagementStub) RevokeDeviceJoinToken(ctx context.Context, input *gen.RevokeDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinTokenSummary, error) {
	return s.revoke(ctx, input)
}

func TestDeviceEnrollmentTokenContracts(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerDeviceEnrollmentTokenTools(live, &DeviceEnrollmentTokenService{})
	registerDeviceEnrollmentTokenTools(unavailable, nil)
	require.Empty(t, live.For(AudienceAssistant))
	for _, name := range []string{"list_device_enrollment_tokens", "revoke_device_enrollment_token"} {
		a, b := descriptorByName(t, live, name), descriptorByName(t, unavailable, name)
		require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
		require.Equal(t, a.Meta, b.Meta)
		require.Equal(t, ExternalAuthorizationMember, a.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
		require.Equal(t, externalOnly, a.Meta.Audiences)
		args := map[string]any{"project_id": uuid.NewString()}
		if name == "revoke_device_enrollment_token" {
			require.True(t, a.Annotations.IdempotentHint)
			require.True(t, *a.Annotations.DestructiveHint)
			require.Equal(t, []authz.Scope{authz.ScopeWorkloadRead, authz.ScopeWorkloadWrite}, a.Meta.DiscoveryScopes)
			args["agent_id"], args["token_id"], args["expected_status"], args["confirmed"] = uuid.NewString(), uuid.NewString(), "active", true
		} else {
			require.True(t, a.Annotations.ReadOnlyHint)
			require.Equal(t, []authz.Scope{authz.ScopeWorkloadRead}, a.Meta.DiscoveryScopes)
		}
		raw, err := json.Marshal(args)
		require.NoError(t, err)
		_, err = b.Invoke(contextWithPrincipal(t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"}), raw)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, unavailableCode)
	}
}

func TestDeviceEnrollmentTokenConfirmedRevocationAndReplay(t *testing.T) {
	t.Parallel()
	p := Principal{OrganizationID: "test-org", UserID: "test-user"}
	project, agent, token := uuid.New(), uuid.New(), uuid.New()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: p.OrganizationID, UserID: p.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, p.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	row := &gen.WorkloadDeviceJoinTokenSummary{ID: token.String(), AgentID: agent.String(), Status: "active", CreatedByUserID: "private-creator", ConsumedBySubject: new("private-subject")}
	reads, writes, mutations := 0, 0, 0
	service := &DeviceEnrollmentTokenService{
		resolveProject: func(_ context.Context, org string, input FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, p.OrganizationID, org)
			require.Equal(t, project.String(), input.ProjectID)
			return ResolvedProject{ID: project, Slug: "explicit"}, nil
		},
		management: deviceEnrollmentManagementStub{
			list: func(scoped context.Context, input *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error) {
				reads++
				require.Equal(t, token.String(), *input.TokenID)
				require.Equal(t, agent.String(), *input.AgentID)
				require.Equal(t, 1, input.Limit)
				ac, ok := contextvalues.GetAuthContext(scoped)
				require.True(t, ok)
				require.Equal(t, project, *ac.ProjectID)
				require.Equal(t, p.UserID, ac.UserID)
				actor, _ := contextvalues.AuthenticatedActor(ctx)
				actual, _ := contextvalues.AuthenticatedActor(scoped)
				require.Equal(t, actor, actual)
				return &gen.WorkloadDeviceJoinTokenList{Items: []*gen.WorkloadDeviceJoinTokenSummary{row}}, nil
			},
			revoke: func(_ context.Context, input *gen.RevokeDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinTokenSummary, error) {
				writes++
				require.Equal(t, token.String(), input.ID)
				if row.Status == "revoked" {
					return nil, oops.C(oops.CodeFailedPrecondition)
				}
				mutations++
				row.Status = "revoked"
				return nil, nil
			},
		},
	}
	input := RevokeDeviceEnrollmentTokenInput{ProjectID: project.String(), AgentID: agent.String(), TokenID: strings.ToUpper(token.String()), ExpectedStatus: "active", Confirmed: true}
	for range 2 {
		result, err := service.revoke(ctx, p, input)
		require.NoError(t, err)
		require.Equal(t, "revoked", result.Status)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private")
	}
	require.Equal(t, 4, reads)
	require.Equal(t, 2, writes)
	require.Equal(t, 1, mutations)
	ac, _ := contextvalues.GetAuthContext(ctx)
	require.Nil(t, ac.ProjectID)
	input.Confirmed = false
	_, err := service.revoke(ctx, p, input)
	require.Error(t, err)
	require.Equal(t, 4, reads)
	input.Confirmed = true
	input.ExpectedStatus = "redeemed"
	_, err = service.revoke(ctx, p, input)
	require.Error(t, err)
	require.Equal(t, 4, reads)
	input.ExpectedStatus = "active"
	_, err = service.revoke(ctx, Principal{OrganizationID: "foreign", UserID: p.UserID}, input)
	require.Error(t, err)
	require.Equal(t, 4, reads)
	_, err = service.revoke(contextvalues.WithLegacyAPIKeyAuthorization(ctx, ac), p, input)
	require.Error(t, err)
	require.Equal(t, 4, reads)
}

func TestDeviceEnrollmentTokenRejectsChangedTargetAndRedemptionRace(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"expired", "redeemed", "wrong-agent", "racing-redemption", "permission-denied"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx, p, project, token := assertionReuseTestContext(t)
			agent := uuid.NewString()
			row := &gen.WorkloadDeviceJoinTokenSummary{ID: token.String(), AgentID: agent, Status: state}
			if state == "wrong-agent" {
				row.AgentID = uuid.NewString()
				row.Status = "active"
			}
			if state == "racing-redemption" || state == "permission-denied" {
				row.Status = "active"
			}
			writes := 0
			service := &DeviceEnrollmentTokenService{resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
				return ResolvedProject{ID: project}, nil
			}, management: deviceEnrollmentManagementStub{
				list: func(context.Context, *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error) {
					return &gen.WorkloadDeviceJoinTokenList{Items: []*gen.WorkloadDeviceJoinTokenSummary{row}}, nil
				},
				revoke: func(context.Context, *gen.RevokeDeviceJoinTokenPayload) (*gen.WorkloadDeviceJoinTokenSummary, error) {
					writes++
					if state == "permission-denied" {
						return nil, oops.C(oops.CodeForbidden)
					}
					row.Status = "redeemed"
					return nil, oops.C(oops.CodeFailedPrecondition)
				},
			}}
			_, err := service.revoke(ctx, p, RevokeDeviceEnrollmentTokenInput{ProjectID: project.String(), AgentID: agent, TokenID: token.String(), ExpectedStatus: "active", Confirmed: true})
			require.Error(t, err)
			if state == "racing-redemption" || state == "permission-denied" {
				require.Equal(t, 1, writes)
			} else {
				require.Zero(t, writes)
			}
		})
	}
}

func TestDeviceEnrollmentTokenListBoundsAndSafeProjection(t *testing.T) {
	t.Parallel()
	ctx, p, project, token := assertionReuseTestContext(t)
	calls := 0
	service := &DeviceEnrollmentTokenService{resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: project}, nil
	}, management: deviceEnrollmentManagementStub{
		list: func(_ context.Context, input *gen.ListDeviceJoinTokensPayload) (*gen.WorkloadDeviceJoinTokenList, error) {
			calls++
			require.Equal(t, 50, input.Limit)
			return &gen.WorkloadDeviceJoinTokenList{Items: []*gen.WorkloadDeviceJoinTokenSummary{{ID: token.String(), Status: "active", CreatedByUserID: "private-creator", ConsumedBySubject: new("private-subject")}}, NextCursor: new("continuation")}, nil
		},
	}}
	result, err := service.list(ctx, p, ListDeviceEnrollmentTokensInput{ProjectID: project.String()})
	require.NoError(t, err)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	require.Equal(t, "continuation", *result.NextCursor)
	for _, limit := range []int{-1, 201} {
		_, err = service.list(ctx, p, ListDeviceEnrollmentTokensInput{ProjectID: project.String(), Limit: limit})
		require.Error(t, err)
	}
	require.Equal(t, 1, calls)
	resultErr, ok := deviceEnrollmentTokenToolResult(errors.New("private backend detail"))
	require.True(t, ok)
	encoded, err := json.Marshal(resultErr)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestDeviceEnrollmentTokenAPIAndMCPRequireSameReadScope(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := workloadpolicy.NewService(logger, testenv.NewTracerProvider(t), nil, &sessions.Manager{}, engine, nil, authserver.Hosts{}, nil)
	ctx, p, project, _ := assertionReuseTestContext(t)
	service := &DeviceEnrollmentTokenService{management: management, resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: project}, nil
	}}
	for _, grants := range [][]authz.Grant{nil, {authz.NewGrant(authz.ScopeWorkloadRead, "foreign")}, {authz.NewGrant(authz.ScopeProjectRead, project.String())}} {
		scoped := authz.GrantsToContext(ctx, grants)
		_, apiErr := management.ListDeviceJoinTokens(scoped, &gen.ListDeviceJoinTokensPayload{Limit: 50})
		_, mcpErr := service.list(scoped, p, ListDeviceEnrollmentTokensInput{ProjectID: project.String()})
		var apiOops, mcpOops *oops.ShareableError
		require.ErrorAs(t, apiErr, &apiOops)
		require.ErrorAs(t, mcpErr, &mcpOops)
		require.Equal(t, oops.CodeForbidden, apiOops.Code)
		require.Equal(t, apiOops.Code, mcpOops.Code)
	}
}
