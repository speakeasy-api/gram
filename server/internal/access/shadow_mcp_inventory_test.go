package access

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

func TestService_ListShadowMCPInventory_ComposesInventoryUsageAndAccessState(t *testing.T) {
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

	allowProjectID := projectID
	_ = createShadowMCPAccessRuleForTest(t, ctx, ti, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		AccessScope:  shadowMCPAccessScopeProject,
		ProjectID:    &allowProjectID,
		MatchBreadth: "full_url",
		MatchValue:   "https://mcp.speakeasy.com/mcp",
		DisplayName:  "Speakeasy Allow",
		Reason:       conv.PtrEmpty("Trusted"),
	})
	_ = createShadowMCPAccessRuleForTest(t, ctx, ti, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "github.example.com",
		DisplayName:  "GitHub Deny",
		Reason:       conv.PtrEmpty("Blocked"),
	})

	var result *gen.ListShadowMCPInventoryResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		result, err = ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
			ProjectID: projectID,
			Limit:     10,
		})
		require.NoError(c, err)
		require.Len(c, result.Servers, 2)
		speakeasy := shadowMCPInventoryServerByURL(result.Servers, "https://mcp.speakeasy.com/mcp")
		require.NotNil(c, speakeasy)
		require.Equal(c, 3, speakeasy.ObservedUseCount)
	}, 5*time.Second, 100*time.Millisecond)

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
	require.Equal(t, shadowMCPInventoryAccessAllowed, speakeasy.ExplicitAccess)
	require.Equal(t, shadowMCPInventoryAccessAllowed, speakeasy.EffectiveAccess)
	require.NotNil(t, speakeasy.ExplicitRule)
	require.Equal(t, "Speakeasy Allow", speakeasy.ExplicitRule.DisplayName)
	require.NotNil(t, speakeasy.EffectiveRule)
	require.Equal(t, "Speakeasy Allow", speakeasy.EffectiveRule.DisplayName)

	github := shadowMCPInventoryServerByURL(result.Servers, "https://github.example.com/mcp")
	require.NotNil(t, github)
	require.NotNil(t, github.ServerName)
	require.Equal(t, "GitHub", *github.ServerName)
	require.Nil(t, github.LastCalled)
	require.Equal(t, 0, github.ObservedUseCount)
	require.Equal(t, 0, github.UserCount)
	require.Empty(t, github.TopUsers)
	require.Equal(t, shadowMCPInventoryAccessNone, github.ExplicitAccess)
	require.Equal(t, shadowMCPInventoryAccessDenied, github.EffectiveAccess)
	require.NotNil(t, github.EffectiveRule)
	require.Equal(t, "GitHub Deny", github.EffectiveRule.DisplayName)
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

	var firstPage *gen.ListShadowMCPInventoryResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		firstPage, err = ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
			ProjectID: projectID,
			Limit:     2,
		})
		require.NoError(c, err)
		require.Len(c, firstPage.Servers, 2)
	}, 5*time.Second, 100*time.Millisecond)
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

	var result *gen.ListShadowMCPInventoryResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		result, err = ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
			ProjectID: projectID,
			Limit:     10,
		})
		require.NoError(c, err)
		require.Len(c, result.Servers, 1)
	}, 5*time.Second, 100*time.Millisecond)

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

type shadowMCPInventoryTelemetryInput struct {
	ProjectID  string
	ServerURL  string
	ServerName string
	UserEmail  string
	ObservedAt time.Time
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
