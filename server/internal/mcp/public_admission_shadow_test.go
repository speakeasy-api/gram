package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestShadowPublicServerAdmission(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{mandatorySurfaceHostedConnect, mandatorySurfaceProxiedConnect, mandatorySurfaceConsent} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			h := newShadowHarness(t, true)
			ctx := t.Context()
			connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
			shadow := h.service.newMandatoryShadow(ctx, shadowOrg)
			shadow.recordServer(ctx, surface, true, authz.MandatoryServerPublic, connect)
			shadow.recordServer(ctx, surface, false, authz.MandatoryServerPrivate, connect)
			shadow.record(ctx, mandatorySurfaceAuthClient, false, authz.MCPAuthClientCheck("client_1", authz.MCPAuthClientDimensions{ClientType: "service_account", ProjectID: "project_1"}))
			require.Equal(t, int64(1), h.points(t)[shadowKey(surface, mandatoryCallerAnonymous, "anonymous", true, "allow", "visibility", "none")])
			require.Equal(t, int64(1), h.points(t)[shadowKey(surface, mandatoryCallerAnonymous, "anonymous", false, "deny", "none", "unauthenticated")])
			require.Equal(t, int64(1), h.points(t)[shadowKey(mandatorySurfaceAuthClient, mandatoryCallerAnonymous, "anonymous", false, "deny", "none", "unauthenticated")], "public access never authorizes credential use")
		})
	}
}

func TestShadowPublicHostedAdmissionUsesVisibility(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, true)
	project, server := uuid.New(), uuid.New()
	toolset := &toolsetsrepo.Toolset{ID: server, ProjectID: project, OrganizationID: shadowOrg}
	_, authenticated, err := h.service.admitHostedRequest(t.Context(), toolset, &hostedServing{isPublic: true}, authz.MCPCheck(authz.ScopeMCPConnect, server.String(), project.String()))
	require.NoError(t, err)
	require.False(t, authenticated)
	require.Equal(t, map[attribute.Set]int64{shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerAnonymous, "anonymous", true, "allow", "visibility", "none"): 1}, h.points(t))
}

func TestShadowPublicProxyAdmissionUsesVisibility(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, true)
	project, server := uuid.New(), uuid.New()
	_, err := h.service.authorizeProxyBackendAccess(t.Context(), testenv.NewLogger(t), mandatorySurfaceProxiedConnect, shadowOrg, project, &mcpserversrepo.McpServer{ID: server, ProjectID: project, Visibility: "public"})
	require.NoError(t, err)
	require.Equal(t, map[attribute.Set]int64{shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerAnonymous, "anonymous", true, "allow", "visibility", "none"): 1}, h.points(t))
}

func TestShadowServerAdmissionNoDecisionOnDisabledOrCanceled(t *testing.T) {
	t.Parallel()
	check := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	off := newShadowHarness(t, false)
	off.service.newMandatoryShadow(t.Context(), shadowOrg).recordServer(t.Context(), mandatorySurfaceHostedConnect, true, authz.MandatoryServerPublic, check)
	require.Empty(t, off.points(t))
	on := newShadowHarness(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	on.service.newMandatoryShadow(ctx, shadowOrg).recordServer(ctx, mandatorySurfaceHostedConnect, true, authz.MandatoryServerPublic, check)
	on.service.newMandatoryShadow(t.Context(), shadowOrg).shadowRequireServer(t.Context(), mandatorySurfaceHostedConnect, errors.New("infrastructure failure"), authz.MandatoryServerPublic, check)
	require.Empty(t, on.points(t))
}

func TestShadowPrivateHostedAdmissionStillDeniesAnonymous(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, true)
	project, server := uuid.New(), uuid.New()
	toolset := &toolsetsrepo.Toolset{ID: server, ProjectID: project, OrganizationID: shadowOrg}
	_, _, err := h.service.admitHostedRequest(t.Context(), toolset, &hostedServing{isPublic: false}, authz.MCPCheck(authz.ScopeMCPConnect, server.String(), project.String()))
	require.Error(t, err)
	require.Equal(t, map[attribute.Set]int64{shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerAnonymous, "anonymous", false, "deny", "none", "unauthenticated"): 1}, h.points(t))
}

func TestShadowPublicPreparationErrorNeverAllows(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, true)
	h.service.prepareMandatory = func(context.Context) (authz.MandatoryAuthorization, error) {
		return authz.MandatoryAuthorization{}, errors.New("admission failed")
	}
	h.service.newMandatoryShadow(t.Context(), shadowOrg).recordServer(t.Context(), mandatorySurfaceHostedConnect, true, authz.MandatoryServerPublic, authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1"))
	for tags := range h.points(t) {
		outcome, ok := tags.Value("gram.authz.mandatory.outcome")
		require.True(t, ok)
		require.Equal(t, "error", outcome.AsString())
	}
	require.Len(t, h.points(t), 1)
}

func TestShadowPrivateAndUnknownProxyAdmission(t *testing.T) {
	t.Parallel()
	for _, visibility := range []string{"private", "unknown"} {
		t.Run(visibility, func(t *testing.T) {
			t.Parallel()
			h := newShadowHarness(t, true)
			_, err := h.service.authorizeProxyBackendAccess(t.Context(), testenv.NewLogger(t), mandatorySurfaceProxiedConnect, shadowOrg, uuid.New(), &mcpserversrepo.McpServer{ID: uuid.New(), Visibility: visibility})
			require.Error(t, err)
			if visibility == "unknown" {
				require.Empty(t, h.points(t))
				return
			}
			require.Equal(t, map[attribute.Set]int64{shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerAnonymous, "anonymous", false, "deny", "none", "unauthenticated"): 1}, h.points(t))
		})
	}
}

func TestMemberToolsetConnectPublicChecksPublicToolsets(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, false)
	toolset := &toolsetsrepo.Toolset{ID: uuid.New(), ProjectID: uuid.New()}
	granted := sessionContext(t, shadowOrg, authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()))
	ungranted := sessionContext(t, shadowOrg)

	allowed, err := h.service.memberToolsetConnectAllowed(ungranted, nil, mandatorySurfaceGatewayMember, true, toolset, true, true)
	require.NoError(t, err)
	require.False(t, allowed, "a public toolset is not open to a caller held to mcp:connect")
	allowed, err = h.service.memberToolsetConnectAllowed(granted, nil, mandatorySurfaceGatewayMember, false, toolset, true, true)
	require.NoError(t, err)
	require.False(t, allowed, "a caller outside the organization never reaches it")
	allowed, err = h.service.memberToolsetConnectAllowed(granted, nil, mandatorySurfaceGatewayMember, true, toolset, true, true)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestShadowMemberToolsetKeepsPrivateBoundary(t *testing.T) {
	t.Parallel()
	h := newShadowHarness(t, true)
	ctx := t.Context()
	shadow := h.service.newMandatoryShadow(ctx, shadowOrg)
	toolset := &toolsetsrepo.Toolset{ID: uuid.New(), ProjectID: uuid.New()}
	allowed, err := h.service.memberToolsetConnectAllowed(ctx, shadow, mandatorySurfaceConsent, false, toolset, true, false)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = h.service.memberToolsetConnectAllowed(ctx, shadow, mandatorySurfaceConsent, false, toolset, false, false)
	require.NoError(t, err)
	require.False(t, allowed, "public member admission never bypasses a private underlying toolset")
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceConsent, mandatoryCallerAnonymous, "anonymous", true, "allow", "visibility", "none"):      1,
		shadowKey(mandatorySurfaceConsent, mandatoryCallerAnonymous, "anonymous", false, "deny", "none", "unauthenticated"): 1,
	}, h.points(t))
}
