package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestListRiskResults_ByChatID_RawWithChatReadGrant covers the scoped bypass:
// a chat_id-filtered request from a caller who separately holds chat:read for
// that exact chat gets the raw match, matching what they could already see by
// loading the chat's transcript.
func TestListRiskResults_ByChatID_RawWithChatReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Bypass Test")})
	require.NoError(t, err)
	policyID, _ := uuid.Parse(policy.ID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	chatIDStr := chatID.String()
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		ChatID: &chatIDStr,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.NotNil(t, result.Results[0].Match, "chat:read for this exact chat should bypass redaction")
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", *result.Results[0].Match)
}

// TestListRiskResults_ByChatID_RedactedWithoutChatReadGrant covers the
// default: org:admin alone (no chat:read for the filtered chat) yields a
// redacted match even when the request is chat_id-scoped.
func TestListRiskResults_ByChatID_RedactedWithoutChatReadGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Redact By Default")})
	require.NoError(t, err)
	policyID, _ := uuid.Parse(policy.ID)
	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	chatIDStr := chatID.String()
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		ChatID: &chatIDStr,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Nil(t, result.Results[0].Match, "match must not ship without a chat:read bypass")
	require.Nil(t, result.Results[0].Spans, "spans must not ship without a chat:read bypass")
	require.NotEmpty(t, result.Results[0].MatchRedacted)
	require.NotContains(t, *result.Results[0].MatchRedacted, "AKIA", "raw secret leaked into redacted output")
}

// TestListRiskResults_RedactedWhenNoChatFilter covers the Risk Events page's
// actual shape: no chat_id filter at all, so there is nothing to bypass on
// even if the caller happens to hold chat:read somewhere.
func TestListRiskResults_RedactedWhenNoChatFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("No Chat Filter")})
	require.NoError(t, err)
	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Nil(t, result.Results[0].Match)
	require.NotEmpty(t, result.Results[0].MatchRedacted)
}

func TestUnmaskRiskResult_HappyPath(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Unmask Happy")})
	require.NoError(t, err)
	policyID, _ := uuid.Parse(policy.ID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	listed, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, listed.Results, 1)
	resultID := listed.Results[0].ID

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)

	unmasked, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: resultID})
	require.NoError(t, err)
	require.Equal(t, resultID, unmasked.ID)
	require.NotNil(t, unmasked.Match)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", *unmasked.Match)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "unmasking a result records an audit event")

	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, "risk_result", rec.SubjectType)
	require.Equal(t, chatID.String(), rec.SubjectSlug, "audit records which chat the unmasked secret came from")
}

// TestUnmaskRiskResult_Forbidden covers a caller with org:admin (able to see
// the redacted list) but no chat:read for the result's chat: unmask must be
// denied even though listing succeeded.
func TestUnmaskRiskResult_Forbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Unmask Forbidden")})
	require.NoError(t, err)
	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	listed, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, listed.Results, 1)

	_, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: listed.Results[0].ID})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestUnmaskRiskResult_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
