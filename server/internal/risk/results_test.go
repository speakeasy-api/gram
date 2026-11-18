package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedChatMessage creates a chat and message for the given project, returning
// the chat ID and message ID. The chat is left without an external user id.
func seedChatMessage(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	return seedChatMessageWithUser(t, ti, projectID, orgID, "")
}

// seedChatMessageWithUser creates a chat and message for the given project,
// returning the chat ID and message ID. A non-empty externalUserID stamps the
// chat's external user id so user_id filtering can be exercised; an empty
// string leaves it unset (NULL).
func seedChatMessageWithUser(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, externalUserID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	chatID, err := uuid.NewV7()
	require.NoError(t, err)

	_, err = ti.chatRepo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		ExternalUserID: pgtype.Text{String: externalUserID, Valid: externalUserID != ""},
	})
	require.NoError(t, err)

	msgID, err := testrepo.New(ti.conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "user",
		Content:   "test message with a secret",
	})
	require.NoError(t, err)

	return chatID, msgID
}

func seedRiskResult(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, policyID uuid.UUID, policyVersion int64, msgID uuid.UUID, found bool) {
	t.Helper()
	ctx := t.Context()

	resultID, err := uuid.NewV7()
	require.NoError(t, err)

	repo := riskrepo.New(ti.conn)
	_, err = repo.InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: policyVersion,
		ChatMessageID:     uuid.NullUUID{UUID: msgID, Valid: true},
		Source:            "gitleaks",
		Found:             found,
		RuleID:            pgtype.Text{String: "aws-access-key-id", Valid: found},
		Description:       pgtype.Text{String: "AWS Access Key ID", Valid: found},
		Match:             pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: found},
		StartPos:          pgtype.Int4{Int32: 0, Valid: found},
		EndPos:            pgtype.Int4{Int32: 20, Valid: found},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: found},
		Tags:              nil,
	}})
	require.NoError(t, err)
}

// A disabled ("turned off") policy still holds the findings it produced while
// active. Both the explicit policy filter and the default view surface them,
// with totals that match the listing.
func TestListRiskResults_ByPolicy_DisabledPolicyShowsHistoricalFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Disabled Policy Test")})
	require.NoError(t, err)

	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "")
	at := time.Now().UTC().Add(-time.Hour)
	finding := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, at, at, "gitleaks", "aws-access-key-id", "", "AKIA**************LE", "", "")
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	// Turn the policy off after it produced the finding.
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      policy.ID,
		Name:    policy.Name,
		Enabled: new(false),
	})
	require.NoError(t, err)

	byPolicy, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, byPolicy.Results, 1, "disabled policy should still show its historical findings when filtered")
	require.Equal(t, "aws-access-key-id", *byPolicy.Results[0].RuleID)
	require.Equal(t, int64(1), byPolicy.TotalCount)

	unfiltered, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, unfiltered.Results, 1, "disabling a policy keeps its history in the default view")
	require.Equal(t, finding.ID.String(), unfiltered.Results[0].ID)
	require.Equal(t, int64(1), unfiltered.TotalCount)
}

// A policy filter must not swallow the other filters. Selecting a policy and a
// rule id together has to honor both, otherwise (as reported in FDE-32) picking
// a policy causes rule_id/user_id/category/time filters to be silently ignored.
func TestListRiskResults_ByPolicyAndRuleID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Policy+Rule Filter")})
	require.NoError(t, err)
	otherPolicy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Other Policy")})
	require.NoError(t, err)

	at := time.Now().UTC().Add(-time.Hour)
	injChat, injMsg := seedChatWithUser(t, ti, projectID, orgID, "")
	injection := chListFinding(t, projectID, orgID, injChat, injMsg, policy.ID, at, at, "prompt_injection", "prompt_injection", "", "", "", "")
	emailChat, emailMsg := seedChatWithUser(t, ti, projectID, orgID, "")
	email := chListFinding(t, projectID, orgID, emailChat, emailMsg, policy.ID, at, at.Add(time.Minute), "presidio", "pii.email_address", "", "***@b.com", "", "")
	// Same rule under another policy: the rule filter alone would match it.
	otherInjection := chListFinding(t, projectID, orgID, injChat, injMsg, otherPolicy.ID, at, at.Add(2*time.Minute), "prompt_injection", "prompt_injection", "", "", "", "")
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{injection, email, otherInjection}))

	ruleID := "prompt_injection"
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
		RuleID:   &ruleID,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1, "policy + rule_id filter should return only the matching rule")
	require.Equal(t, injection.ID.String(), result.Results[0].ID)
}

func TestListRiskResults_ByChatID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	active, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Active Chat Policy")})
	require.NoError(t, err)
	disabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Disabled Chat Policy"), Enabled: new(false)})
	require.NoError(t, err)
	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	at := time.Now().UTC().Add(-time.Hour)
	live := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, active.ID, at, at, "gitleaks", "secret.test", "", "***", "", "")
	disabledFinding := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, disabled.ID, at, at, "gitleaks", "secret.disabled", "", "***", "", "")
	excluded := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, active.ID, at, at, "gitleaks", "secret.excluded", "", "***", "", "")
	excluded.ExcludedAt = &at
	dismissed := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, active.ID, at, at, "gitleaks", "secret.dismissed", "", "***", "", "")
	dismissed.FalsePositiveAt = &at
	shadow := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, active.ID, at, at, "gitleaks", "secret.shadow", "", "***", "", "")
	shadow.Shadow = true
	deadLetter := chListFinding(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, chatID, msgID, active.ID, at, at, "gitleaks", "secret.dead", "", "***", "", "")
	deadLetter.DeadLetterReason = "fixture"
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{live, disabledFinding, excluded, dismissed, shadow, deadLetter}))

	chatIDString := chatID.String()
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{ChatID: &chatIDString})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, live.ID.String(), result.Results[0].ID)
	require.Equal(t, int64(1), result.TotalCount)
	disabledOnly, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{ChatID: &chatIDString, PolicyID: &disabled.ID})
	require.NoError(t, err)
	require.Empty(t, disabledOnly.Results)
	require.Zero(t, disabledOnly.TotalCount)
}

func TestListRiskResults_ByChatID_IncludesContentPartFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Content Part Chat Filter Test")})
	require.NoError(t, err)

	chatID, _ := seedChatWithUser(t, ti, projectID, orgID, "")
	partID := uuid.Must(uuid.NewV7())
	at := time.Now().UTC().Add(-time.Hour)
	finding := chListFinding(t, projectID, orgID, chatID, uuid.Nil, policy.ID, at, at, "gitleaks", "generic-api-key", "", "<redacted>", "", "")
	finding.ChatMessageID = ""
	finding.ContentPartID = partID.String()
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	chatIDStr := chatID.String()
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{ChatID: &chatIDStr})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	got := result.Results[0]
	require.Nil(t, got.ChatMessageID)
	require.Equal(t, partID.String(), *got.ChatContentPartID)
	require.Equal(t, chatIDStr, *got.ChatID)
}

func TestListRiskResults_ProjectIncludesContentPartFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Content Part Project List Test")})
	require.NoError(t, err)

	const secret = "SECRET_ATTACHMENT_TOKEN"
	chatID, _ := seedChatWithUser(t, ti, projectID, orgID, "")
	partID := uuid.Must(uuid.NewV7())
	at := time.Now().UTC().Add(-time.Hour)
	finding := chListFinding(t, projectID, orgID, chatID, uuid.Nil, policy.ID, at, at, "gitleaks", "generic-api-key", "", maskdisplay.Display("gitleaks", "generic-api-key", secret), "", "")
	finding.ChatMessageID = ""
	finding.ContentPartID = partID.String()
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	got := result.Results[0]
	require.Nil(t, got.ChatMessageID)
	require.NotNil(t, got.ChatContentPartID)
	require.Equal(t, partID.String(), *got.ChatContentPartID)
	require.Equal(t, chatID.String(), *got.ChatID)
	require.Nil(t, got.Match, "project-level list remains redacted without chat:read")
	require.Nil(t, got.Spans)
	require.NotNil(t, got.MatchRedacted)
	require.NotContains(t, *got.MatchRedacted, secret)
}

func TestListRiskResults_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListRiskPolicies_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestGetRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestUpdateRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{ID: uuid.New().String(), Name: "x"})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeleteRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	err := ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// The agent surface passes the ingest-time store redaction through untouched:
// raw match content never reaches it.
func TestListRiskResultsForAgent_RedactsGitleaksMatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Agent Redact")})
	require.NoError(t, err)

	const secret = "AKIAIOSFODNN7EXAMPLE"
	display := maskdisplay.Display("gitleaks", "aws-access-key-id", secret)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "")
	at := time.Now().UTC().Add(-time.Hour)
	finding := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, at, at, "gitleaks", "aws-access-key-id", "", display, "", "")
	finding.StartPos = 0
	finding.EndPos = int32(len(secret))
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	result, err := ti.service.ListRiskResultsForAgent(ctx, &gen.ListRiskResultsForAgentPayload{
		PolicyID: &policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	got := result.Results[0]
	require.Equal(t, display, got.MatchRedacted)
	require.NotEqual(t, secret, got.MatchRedacted, "raw secret leaked into redacted output")
	require.True(t, got.PositionKnown)
	require.Equal(t, "aws-access-key-id", *got.RuleID)
}

func TestListRiskResultsForAgent_ShadowMCPPassthrough(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow Passthrough"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	const shadowMatch = "mcp__evil-server__"
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "")
	at := time.Now().UTC().Add(-time.Hour)
	finding := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, at, at, "shadow_mcp", "unapproved-mcp", "", maskdisplay.Display("shadow_mcp", "unapproved-mcp", shadowMatch), "", "")
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	result, err := ti.service.ListRiskResultsForAgent(ctx, &gen.ListRiskResultsForAgentPayload{
		PolicyID: &policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, shadowMatch, result.Results[0].MatchRedacted, "shadow_mcp match should pass through verbatim")
}

func TestListRiskResultsForAgent_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskResultsForAgent(ctx, &gen.ListRiskResultsForAgentPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
