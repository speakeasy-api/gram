package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAuditPagesRefreshLiveMembershipAndGrants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "audit_live_authority")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	principal.ClientID = "audit-test-client"
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleMember)
	for range 60 {
		for _, user := range []string{principal.UserID, "other-user"} {
			_, err := auditrepo.New(conn).InsertAuditLog(ctx, auditrepo.InsertAuditLogParams{
				OrganizationID: principal.OrganizationID, ActorID: user, ActorType: "user", Action: "project:update", SubjectID: "project", SubjectType: "project",
			})
			require.NoError(t, err)
		}
	}
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	tracer := testenv.NewTracerProvider(t)
	redis, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracer, conn, redis, cache.Suffix("audit-live-test"), billing.NewStubClient(logger, tracer))
	service := auditapi.NewService(logger, tracer, conn, sessions, engine)
	reader := NewPostgresReader(logger, conn).WithAuditLogs(service)
	runtime := NewRuntime(logger, &testAuthenticator{principal: principal}, testGate{enabled: true}, NewLiveOrgAdminAuthorizer(conn, engine), "", "audit-test-key", reader, nil, nil, nil, nil)
	handler := runtime.Handler()
	call := func(cursor string, actorIDs ...string) (int, bool, listAuditLogEntriesOutput) {
		t.Helper()
		arguments := map[string]any{"cursor": cursor}
		if len(actorIDs) > 0 {
			arguments["actor_id"] = actorIDs[0]
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "list_audit_log_entries", "arguments": arguments}})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		// Unrelated parent authority must never survive external admission.
		req = req.WithContext(authz.GrantsToContext(req.Context(), []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			return res.Code, true, listAuditLogEntriesOutput{}
		}
		var response struct {
			Result struct {
				IsError           bool                      `json:"isError"`
				StructuredContent listAuditLogEntriesOutput `json:"structuredContent"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response), res.Body.String())
		return res.Code, response.Result.IsError, response.Result.StructuredContent
	}
	status, refused, page := call("")
	require.Equal(t, http.StatusOK, status)
	require.False(t, refused)
	require.Equal(t, "self", page.Visibility)
	require.Len(t, page.Entries, 50)
	require.NotNil(t, page.NextCursor)
	cursor := *page.NextCursor
	_, refused, own := call(cursor, principal.UserID)
	require.False(t, refused)
	require.Len(t, own.Entries, 10)
	_, refused, conflicting := call(cursor, "other-user")
	require.True(t, refused)
	require.Empty(t, conflicting.Entries)
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleAdmin)
	_, refused, filtered := call("", "other-user")
	require.False(t, refused)
	require.Len(t, filtered.Entries, 50)
	require.NotNil(t, filtered.NextCursor)
	for _, entry := range filtered.Entries {
		require.Equal(t, "other-user", entry.ActorID)
	}
	_, refused, promoted := call(cursor)
	require.False(t, refused)
	require.Equal(t, "organization", promoted.Visibility)
	require.Greater(t, len(promoted.Entries), 10)
	for _, entry := range promoted.Entries {
		for _, newer := range page.Entries {
			require.NotEqual(t, newer.ID, entry.ID, "promotion cannot replay newer events")
		}
	}
	_, err = accessrepo.New(conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: principal.OrganizationID, WorkosUserID: principal.UserID})
	require.NoError(t, err)
	seedPlatformMCPAuthorizationMember(t, ctx, conn, principal.OrganizationID, principal.UserID, authz.SystemRoleMember)
	_, refused, oldFilter := call(*filtered.NextCursor, "other-user")
	require.True(t, refused, "an old cursor and actor filter cannot retain admin visibility")
	require.Empty(t, oldFilter.Entries)
	require.Nil(t, oldFilter.NextCursor)
	_, refused, demoted := call(cursor)
	require.False(t, refused)
	require.Equal(t, "self", demoted.Visibility)
	require.Len(t, demoted.Entries, 10)
	for _, entry := range demoted.Entries {
		require.Equal(t, principal.UserID, entry.ActorID)
	}
	// Directory-mapped roles participate in the same fresh effective snapshot.
	role := seedAccessRole(t, ctx, conn, principal.OrganizationID, "audit-investigator", "Audit Investigator")
	now := conv.ToPGTimestamptz(time.Now().UTC())
	_, err = directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID), WorkosDirectoryUserID: "audit-directory-user",
		Email: conv.ToPGText(principal.UserID + "@example.test"), Attributes: []byte(`{"department_name":"Audit"}`),
		WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: pgtype.Text{}, RestoreDeleted: false,
	})
	require.NoError(t, err)
	_, err = accessrepo.New(conn).InsertDirectoryAttributeRoleMapping(ctx, accessrepo.InsertDirectoryAttributeRoleMappingParams{
		OrganizationID: principal.OrganizationID, AttributeKey: conv.ToPGText("department_name"), AttributeValue: conv.ToPGText("Audit"), RoleUrn: role.String(),
	})
	require.NoError(t, err)
	grant := func(principalURN urn.Principal, scope authz.Scope) uuid.UUID {
		t.Helper()
		selector, err := authz.NewSelector(scope, principal.OrganizationID).MarshalJSON()
		require.NoError(t, err)
		row, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: principal.OrganizationID, PrincipalUrn: principalURN, Scope: string(scope), Selectors: selector,
		})
		require.NoError(t, err)
		return row.ID
	}
	directoryAdmin := grant(role, authz.ScopeOrgAdmin)
	_, refused, mapped := call(cursor)
	require.False(t, refused)
	require.Equal(t, "organization", mapped.Visibility)
	blockedAdmin := grant(role, authz.ScopeOrgBlockedAdmin)
	_, refused, excluded := call(cursor)
	require.False(t, refused)
	require.Equal(t, "self", excluded.Visibility)
	directAdmin := grant(urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), authz.ScopeOrgAdmin)
	_, refused, direct := call(cursor)
	require.False(t, refused)
	require.Equal(t, "organization", direct.Visibility, "direct-user grants take precedence over role exclusions")
	for _, id := range []uuid.UUID{directAdmin, directoryAdmin, blockedAdmin} {
		_, err = accessrepo.New(conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: id, OrganizationID: principal.OrganizationID})
		require.NoError(t, err)
	}
	// Removing inherited read cannot be repaired by a foreign-org direct grant.
	_, err = accessrepo.New(conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: principal.OrganizationID, WorkosUserID: principal.UserID})
	require.NoError(t, err)
	selector, err := authz.NewSelector(authz.ScopeOrgRead, "other-org").MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: principal.OrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), Scope: string(authz.ScopeOrgRead), Selectors: selector,
	})
	require.NoError(t, err)
	_, refused, revoked := call(cursor)
	require.True(t, refused)
	require.Empty(t, revoked.Entries)
	require.Nil(t, revoked.NextCursor)
	require.NoError(t, organizationsrepo.New(conn).DeleteOrganizationUserRelationship(ctx, organizationsrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID),
	}))
	status, refused, _ = call(cursor)
	require.Equal(t, http.StatusForbidden, status)
	require.True(t, refused)
}
