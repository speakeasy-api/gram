package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func saveAudience(t *testing.T, ctx context.Context, ti *testInstance, resourceID string, entries ...*gen.SetResourceAudienceEntry) *gen.ResourceAudienceResult {
	t.Helper()

	result, err := ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
		ResourceKind:    "mcp",
		ResourceID:      resourceID,
		Entries:         entries,
		ExpectedVersion: currentAudienceVersion(t, ctx, ti, resourceID),
		SessionToken:    nil,
		ApikeyToken:     nil,
	})
	require.NoError(t, err)
	return result
}

func connectDecisions(audience *gen.ResourceConnectAudience) map[string]string {
	decisions := make(map[string]string, len(audience.Members))
	for _, member := range audience.Members {
		decisions[member.UserID] = member.Decision + "/" + member.ToolAccess
	}
	return decisions
}

func userURN(userID string) string {
	return urn.NewPrincipal(urn.PrincipalTypeUser, userID).String()
}

// The connect audience is the decision the MCP endpoint makes, member by
// member: a block a member inherits from everyone beats every grant except a
// direct one naming this server, and a block a member holds themselves beats
// all of their own grants.
func TestService_ListResourceAudience_ConnectAudienceAppliesBlocksAndOverrides(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	alice := seedOrgMember(t, ctx, ti, orgID, "alice@example.com")
	bob := seedOrgMember(t, ctx, ti, orgID, "bob@example.com")
	carol := seedOrgMember(t, ctx, ti, orgID, "carol@example.com")

	// Everyone is blocked; Alice is named directly, so her grant outranks the
	// inherited block. Bob and Carol, and the administrator whose all-server
	// connect comes from a role, are blocked.
	result := saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: "*", Level: "blocked"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "use"},
	)
	require.NotNil(t, result.ConnectAudience)
	require.Equal(t, "private", result.ConnectAudience.Visibility)
	decisions := connectDecisions(result.ConnectAudience)
	require.Equal(t, "allowed_over_block/all", decisions[alice])
	require.Equal(t, "blocked/none", decisions[bob])
	require.Equal(t, "blocked/none", decisions[carol])
	require.Equal(t, "blocked/none", decisions[authCtx.UserID])

	// Everyone may connect except Bob, whose own block beats his share of the
	// everyone grant.
	saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: "*", Level: "use"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(bob), Level: "blocked"},
	)
	listed, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{ResourceKind: "mcp", ResourceID: serverID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	decisions = connectDecisions(listed.ConnectAudience)
	require.Equal(t, "allowed/all", decisions[alice])
	require.Equal(t, "blocked/none", decisions[bob])
	require.Equal(t, "allowed/all", decisions[carol])
	require.Equal(t, "allowed/all", decisions[authCtx.UserID])
}

// A member whose only rule is narrowed to some tools can connect to those
// tools alone, and a member no rule reaches is not listed.
func TestService_ListResourceAudience_ConnectAudienceReportsNarrowedAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	alice := seedOrgMember(t, ctx, ti, orgID, "alice@example.com")
	bob := seedOrgMember(t, ctx, ti, orgID, "bob@example.com")

	result := saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "use", Tools: []string{"search"}},
	)
	decisions := connectDecisions(result.ConnectAudience)
	require.Equal(t, "allowed/some", decisions[alice])
	require.NotContains(t, decisions, bob)
}

// A gateway has no connect check of its own, so it reports no audience
// rather than one the endpoint would not enforce.
func TestService_ListResourceAudience_GatewayHasNoConnectAudience(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	gatewayID := seedMCPGateway(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	result, err := ti.service.ListResourceAudience(ctx, &gen.ListResourceAudiencePayload{ResourceKind: "mcp", ResourceID: gatewayID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Nil(t, result.ConnectAudience)
}

// Every save that changes a server's audience leaves one audit entry with the
// rules before and after and the connect changes spelled out; a save that
// changes nothing leaves none.
func TestService_SetResourceAudience_RecordsAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	alice := seedOrgMember(t, ctx, ti, orgID, "alice@example.com")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessResourceAudienceUpdate)
	require.NoError(t, err)

	saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "use"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: "*", Level: "blocked"},
	)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessResourceAudienceUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionAccessResourceAudienceUpdate, serverID)
	require.NoError(t, err)
	require.Equal(t, "access_resource_audience", record.SubjectType)
	require.Equal(t, authCtx.UserID, record.ActorID)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{userURN(alice)}, metadata["connect_granted"])
	require.Equal(t, []any{"user:all"}, metadata["connect_blocked"])
	require.Empty(t, metadata["connect_revoked"])
	require.Empty(t, metadata["connect_unblocked"])
	require.NotEmpty(t, record.AfterSnapshot)

	// Re-saving the same rules is not a change.
	saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "use"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: "*", Level: "blocked"},
	)
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessResourceAudienceUpdate)
	require.NoError(t, err)
	require.Equal(t, after, unchanged)
}

// SetResourceConnectRule changes one principal's whole-server connect rule and
// nothing else. Alice's other rule is a manage block rather than a view grant
// because view still implies connect until that expansion is removed: other principals' rules, the principal's other levels, and a
// narrower block they hold all survive.
func TestRoleManager_SetResourceConnectRule_ChangesOnlyOneConnectRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: nil}
	roles := ti.service.roleMgr

	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	alice := seedOrgMember(t, ctx, ti, orgID, "alice@example.com")
	bob := seedOrgMember(t, ctx, ti, orgID, "bob@example.com")
	aliceURN := urn.NewPrincipal(urn.PrincipalTypeUser, alice)

	saveAudience(t, ctx, ti, serverID,
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(bob), Level: "manage"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "blocked_manage"},
		&gen.SetResourceAudienceEntry{PrincipalUrn: userURN(alice), Level: "blocked", Dispositions: []string{"destructive"}},
	)

	read, err := roles.ReadResourceConnectionAudience(ctx, orgID, serverID)
	require.NoError(t, err)

	rulesOf := func(audience ResourceConnectionAudience) map[string]string {
		rules := make(map[string]string, len(audience.Rules))
		for _, rule := range audience.Rules {
			value := "whole"
			if len(rule.Dispositions) > 0 || len(rule.Tools) > 0 {
				value = "narrowed"
			}
			rules[rule.PrincipalUrn+"|"+rule.Level] = value
		}
		return rules
	}

	allowed, err := roles.SetResourceConnectRule(ctx, orgID, actor, serverID, aliceURN, ConnectRuleAllowed, read.Version)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		userURN(bob) + "|manage":           "whole",
		userURN(alice) + "|blocked_manage": "whole",
		userURN(alice) + "|use":            "whole",
		userURN(alice) + "|blocked":        "narrowed",
	}, rulesOf(allowed))
	require.Equal(t, "allowed/some", connectDecisions(allowed.Audience)[alice])

	blocked, err := roles.SetResourceConnectRule(ctx, orgID, actor, serverID, aliceURN, ConnectRuleBlocked, allowed.Version)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		userURN(bob) + "|manage":           "whole",
		userURN(alice) + "|blocked_manage": "whole",
		userURN(alice) + "|blocked":        "whole",
	}, rulesOf(blocked))
	require.Equal(t, "blocked/none", connectDecisions(blocked.Audience)[alice])

	cleared, err := roles.SetResourceConnectRule(ctx, orgID, actor, serverID, aliceURN, ConnectRuleNotGranted, blocked.Version)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		userURN(bob) + "|manage":           "whole",
		userURN(alice) + "|blocked_manage": "whole",
	}, rulesOf(cleared))
	require.NotContains(t, connectDecisions(cleared.Audience), alice)

	// A stale version is a conflict, not an overwrite.
	_, err = roles.SetResourceConnectRule(ctx, orgID, actor, serverID, aliceURN, ConnectRuleAllowed, read.Version)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeFailedPrecondition, oopsErr.Code)
}

func TestRoleManager_SetResourceConnectRule_RejectsAgentsAndUnknownRules(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), DisplayName: nil}

	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	version := currentAudienceVersion(t, ctx, ti, serverID)

	_, err := ti.service.roleMgr.SetResourceConnectRule(ctx, orgID, actor, serverID, urn.NewPrincipal(urn.PrincipalTypeAgent, "00000000-0000-0000-0000-000000000001"), ConnectRuleAllowed, version)
	require.Error(t, err)

	_, err = ti.service.roleMgr.SetResourceConnectRule(ctx, orgID, actor, serverID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), "manage", version)
	require.Error(t, err)
}
