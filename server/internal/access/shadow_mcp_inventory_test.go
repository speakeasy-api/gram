package access

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListShadowMCPInventory_ComposesInventoryUsageAndPolicyState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	otherProject := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://github.example.com/mcp",
			URLHost:            "github.example.com",
			ServerName:         "GitHub",
			SeenAt:             now.Add(-2 * time.Hour),
			FirstSeen:          now.Add(-2 * time.Hour),
			LastSeen:           now.Add(-2 * time.Hour),
			UpdatedAt:          now.Add(-2 * time.Hour),
		},
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://mcp.speakeasy.com/mcp",
			URLHost:            "mcp.speakeasy.com",
			ServerName:         "Speakeasy",
			SeenAt:             now.Add(-1 * time.Hour),
			FirstSeen:          now.Add(-1 * time.Hour),
			LastSeen:           now.Add(-1 * time.Hour),
			UpdatedAt:          now.Add(-1 * time.Hour),
		},
		{
			GramProjectID:      otherProject.ID.String(),
			CanonicalServerURL: "https://other-project.example.com/mcp",
			URLHost:            "other-project.example.com",
			ServerName:         "Other Project",
			SeenAt:             now,
			FirstSeen:          now,
			LastSeen:           now,
			UpdatedAt:          now,
		},
	}))

	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=one",
		ServerName: "Speakeasy",
		UserEmail:  "alex@example.com",
		ObservedAt: now.Add(-30 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp?token=two#ignored",
		ServerName: "Speakeasy",
		UserEmail:  "alex@example.com",
		ObservedAt: now.Add(-20 * time.Minute),
	})
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://mcp.speakeasy.com/mcp",
		ServerName: "Speakeasy",
		UserEmail:  "sam@example.com",
		ObservedAt: now.Add(-10 * time.Minute),
	})

	blockPolicy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Block Shadow MCP",
		Action:         "block",
	})
	flagPolicy := createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           "Flag Shadow MCP",
		Action:         "flag",
	})
	grantShadowMCPInventoryBypass(t, ctx, ti, authCtx.ActiveOrganizationID, blockPolicy.ID.String(), "https://mcp.speakeasy.com/mcp")
	requestID := createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       blockPolicy.ID.String(),
		ServerURL:      "https://github.example.com/mcp",
		RequesterID:    authCtx.UserID,
		RequesterEmail: "alex@example.com",
		RequestedAt:    now.Add(-5 * time.Minute),
	})
	_ = createShadowMCPInventoryBypassRequest(t, ctx, ti, shadowMCPInventoryBypassRequestInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		PolicyID:       flagPolicy.ID.String(),
		ServerURL:      "https://github.example.com/mcp",
		RequesterID:    "user_flagged",
		RequesterEmail: "flagged@example.com",
		RequestedAt:    now.Add(-4 * time.Minute),
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	require.Nil(t, result.NextCursor)
	require.Len(t, result.Servers, 2)
	require.Nil(t, shadowMCPInventoryServerByURL(result.Servers, "https://other-project.example.com/mcp"))

	speakeasy := shadowMCPInventoryServerByURL(result.Servers, "https://mcp.speakeasy.com/mcp")
	require.NotNil(t, speakeasy)
	require.NotNil(t, speakeasy.ServerName)
	require.Equal(t, "Speakeasy", *speakeasy.ServerName)
	require.Equal(t, "mcp.speakeasy.com", speakeasy.URLHost)
	require.NotEmpty(t, speakeasy.FirstSeen)
	require.NotEmpty(t, speakeasy.LastSeen)
	require.NotNil(t, speakeasy.LastCalled)
	require.Equal(t, 3, speakeasy.ObservedUseCount)
	require.Equal(t, 2, speakeasy.UserCount)
	require.Equal(t, []string{"alex@example.com", "sam@example.com"}, speakeasy.TopUsers)
	require.Equal(t, shadowMCPInventoryAccessAllowed, speakeasy.Access)
	require.Equal(t, 0, speakeasy.RequestCount)
	require.Nil(t, speakeasy.LatestRequest)
	require.Equal(t, []string{blockPolicy.ID.String()}, speakeasy.AllowedPolicyIds)

	github := shadowMCPInventoryServerByURL(result.Servers, "https://github.example.com/mcp")
	require.NotNil(t, github)
	require.NotNil(t, github.ServerName)
	require.Equal(t, "GitHub", *github.ServerName)
	require.Nil(t, github.LastCalled)
	require.Equal(t, 0, github.ObservedUseCount)
	require.Equal(t, 0, github.UserCount)
	require.Empty(t, github.TopUsers)
	require.Equal(t, shadowMCPInventoryAccessBlocked, github.Access)
	require.Equal(t, 1, github.RequestCount)
	require.NotNil(t, github.LatestRequest)
	require.Equal(t, requestID, github.LatestRequest.ID)
	require.Equal(t, blockPolicy.ID.String(), github.LatestRequest.PolicyID)
	require.Equal(t, "alex@example.com", github.LatestRequest.RequesterEmail)
	require.Empty(t, github.AllowedPolicyIds)
}

func TestService_ListShadowMCPInventory_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ch := telemetryRepo.New(ti.chConn)
	now := time.Now().UTC()
	for i, url := range []string{
		"https://one.example.com/mcp",
		"https://two.example.com/mcp",
		"https://three.example.com/mcp",
	} {
		require.NoError(t, ch.UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
			{
				GramProjectID:      projectID,
				CanonicalServerURL: url,
				URLHost:            strings.TrimPrefix(strings.TrimSuffix(url, "/mcp"), "https://"),
				ServerName:         url,
				SeenAt:             now.Add(time.Duration(i) * time.Minute),
				FirstSeen:          now.Add(time.Duration(i) * time.Minute),
				LastSeen:           now.Add(time.Duration(i) * time.Minute),
				UpdatedAt:          now.Add(time.Duration(i) * time.Minute),
			},
		}))
	}

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	firstPage, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     2,
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Servers, 2)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     2,
		Cursor:    firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Servers, 1)
	require.Nil(t, secondPage.NextCursor)
}

func TestService_ListShadowMCPInventory_ServerNameIsOptional(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	now := time.Now().UTC()
	require.NoError(t, telemetryRepo.New(ti.chConn).UpsertShadowMCPInventoryURLs(ctx, []telemetryRepo.UpsertShadowMCPInventoryURLParams{
		{
			GramProjectID:      projectID,
			CanonicalServerURL: "https://unnamed.example.com/mcp",
			URLHost:            "unnamed.example.com",
			ServerName:         "",
			SeenAt:             now,
			FirstSeen:          now,
			LastSeen:           now,
			UpdatedAt:          now,
		},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)

	require.Nil(t, result.Servers[0].ServerName)
}

func TestService_ListShadowMCPInventory_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListShadowMCPInventory_BackendFailureIsUnexpected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	require.NoError(t, ti.chConn.Close())

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

func TestService_ListShadowMCPInventory_InvalidCursorIsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	cursor := "not-a-valid-cursor"

	_, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: authCtx.ProjectID.String(),
		Limit:     10,
		Cursor:    &cursor,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

type shadowMCPInventoryTelemetryInput struct {
	ProjectID  string
	ServerURL  string
	ServerName string
	UserEmail  string
	ObservedAt time.Time
}

type shadowMCPInventoryPolicyInput struct {
	OrganizationID string
	ProjectID      string
	Name           string
	Action         string
}

type shadowMCPInventoryBypassRequestInput struct {
	OrganizationID string
	ProjectID      string
	PolicyID       string
	ServerURL      string
	RequesterID    string
	RequesterEmail string
	RequestedAt    time.Time
}

func createShadowMCPInventoryPolicy(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryPolicyInput) riskrepo.RiskPolicy {
	t.Helper()

	projectID, err := uuid.Parse(input.ProjectID)
	require.NoError(t, err)

	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: input.OrganizationID,
		Name:           input.Name,
		Sources:        []string{"shadow_mcp"},
		Enabled:        true,
		Action:         input.Action,
		AudienceType:   "everyone",
		AutoName:       false,
	})
	require.NoError(t, err)

	return policy
}

func grantShadowMCPInventoryBypass(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, policyID string, serverURL string) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   selector,
	}))
}

func createShadowMCPInventoryBypassRequest(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryBypassRequestInput) string {
	t.Helper()

	projectID, err := uuid.Parse(input.ProjectID)
	require.NoError(t, err)
	policyID, err := uuid.Parse(input.PolicyID)
	require.NoError(t, err)
	dimensions, err := json.Marshal(map[string]string{authz.SelectorKeyServerURL: input.ServerURL})
	require.NoError(t, err)
	requestedAt := input.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now()
	}

	requestID := uuid.New()
	_, err = riskrepo.New(ti.conn).UpsertRiskPolicyBypassRequest(ctx, riskrepo.UpsertRiskPolicyBypassRequestParams{
		ID:               requestID,
		OrganizationID:   input.OrganizationID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		TargetKind:       conv.ToPGText("shadow_mcp_server"),
		TargetLabel:      conv.ToPGText(input.ServerURL),
		TargetKey:        conv.ToPGText(input.ServerURL),
		TargetDimensions: dimensions,
		RequesterUserID:  input.RequesterID,
		RequesterEmail:   conv.ToPGText(input.RequesterEmail),
		Note:             conv.PtrToPGText(nil),
		Status:           "requested",
	})
	require.NoError(t, err)

	err = testrepo.New(ti.conn).UpdateRiskPolicyBypassRequestTimestamps(ctx, testrepo.UpdateRiskPolicyBypassRequestTimestampsParams{
		RequestedAt: conv.ToPGTimestamptz(requestedAt),
		ID:          requestID,
		ProjectID:   projectID,
	})
	require.NoError(t, err)

	return requestID.String()
}

func insertShadowMCPInventoryTelemetry(t *testing.T, ctx context.Context, ti *testInstance, input shadowMCPInventoryTelemetryInput) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":     "hook",
		"gram.hook.source":      "claude-code",
		"gram.mcp.server_url":   input.ServerURL,
		"gram.tool_call.source": input.ServerName,
		"gram.tool.name":        "mcp__speakeasy__search",
		"user.email":            input.UserEmail,
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	spanID := uuid.New().String()[:16]
	traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	err = telemetryRepo.New(ti.chConn).InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         input.ObservedAt.UnixNano(),
		ObservedTimeUnixNano: input.ObservedAt.UnixNano(),
		SeverityText:         nil,
		Body:                 "shadow mcp inventory api call",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        input.ProjectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "hooks:mcp__speakeasy__search",
		ServiceName:          "gram-hooks",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}

func shadowMCPInventoryServerByURL(servers []*gen.ShadowMCPInventoryServer, canonicalURL string) *gen.ShadowMCPInventoryServer {
	for _, server := range servers {
		if server.CanonicalServerURL == canonicalURL {
			return server
		}
	}
	return nil
}
