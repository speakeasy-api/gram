package authz

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestChallengeLogger_skipsWithoutAuthContext(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(t.Context(), conn, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Empty(t, listChallengeOutboxRows(t, conn))
}

func TestChallengeLogger_skipsWhenImpersonating(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.WithValidatedSupportSession(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user_admin",
		AccountType:           "enterprise",
		IsAdmin:               true,
		SupportOrganizationID: orgID,
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_impersonated"}
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Empty(t, listChallengeOutboxRows(t, conn))
}

func TestChallengeLogger_publishesUserPrincipal(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	sessionID := "session_user_principal"
	email := "principal@example.com"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_principal",
		ExternalUserID:       "ext_principal",
		SessionID:            &sessionID,
		Email:                &email,
		AccountType:          "enterprise",
	})
	ctx = GrantsToContext(ctx, []Grant{
		{PrincipalUrn: "role:admin", Scope: ScopeProjectRead, Selector: NewSelector(ScopeProjectRead, WildcardResource)},
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_user"}
	challengeRow{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              []Check{check},
		Focus:               &check,
		Matches:             []grantMatch{{Grant: Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectRead, Selector: NewSelector(ScopeProjectRead, WildcardResource)}, ViaCheck: check}},
		EvaluatedGrantCount: 1,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	require.Equal(t, string(proto.MessageName(&authzv1.Challenge{})), rows[0].Topic)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, rows[0].PublicID.String(), message.GetId())
	require.Equal(t, orgID, message.GetOrganizationId())
	require.Equal(t, "user:user_principal", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeUser), message.GetPrincipalType())
	require.Equal(t, "user_principal", message.GetUserId())
	require.Equal(t, "ext_principal", message.GetUserExternalId())
	require.Equal(t, email, message.GetUserEmail())
	require.Equal(t, sessionID, message.GetSessionId())
	require.Equal(t, []string{"admin"}, message.GetRoleSlugs())
	require.Equal(t, string(authzrepo.OperationRequire), message.GetOperation())
	require.Equal(t, string(authzrepo.OutcomeAllow), message.GetOutcome())
	require.Equal(t, string(authzrepo.ReasonGrantMatched), message.GetReason())
	require.Equal(t, string(ScopeProjectRead), message.GetScope())
	require.Equal(t, "project", message.GetResourceKind())
	require.Equal(t, "proj_user", message.GetResourceId())
	require.Equal(t, uint32(1), message.GetEvaluatedGrantCount())
}

func TestChallengeLogger_publishesAfterRequestCanceled(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_canceled",
		AccountType:          "enterprise",
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	ctx, cancel := context.WithCancel(ctx)
	cancel()

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_canceled"}
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(ctx, conn, testenv.NewLogger(t), func(ctx context.Context, _ string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("flag check: %w", err)
		}
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return true, nil
	})

	require.Len(t, listChallengeOutboxRows(t, conn), 1)
}

func TestChallengeLogger_publishesAPIKeyPrincipal(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_owner",
		APIKeyID:             "key_abc",
		AccountType:          "enterprise",
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_apikey"}
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, "api_key:key_abc", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeAPIKey), message.GetPrincipalType())
	require.Equal(t, "key_abc", message.GetApiKeyId())
	require.Equal(t, "user_owner", message.GetUserId())
}

func TestAuthorizationChallengePrincipalUsesCanonicalAgentActor(t *testing.T) {
	t.Parallel()

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	authCtx := &contextvalues.AuthContext{UserID: "", APIKeyID: "key_abc"}
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), authCtx, agent, contextvalues.PrincipalCredential{
		AuthorizerUserID: "user_authorizer", DelegatedGrants: []byte(`{"requested":[],"effective":[]}`), DelegatedGrantsVersion: 1,
	})

	principalURN, principalType := authorizationChallengePrincipal(ctx, authCtx)
	require.Equal(t, agent.String(), principalURN)
	require.Equal(t, authzrepo.PrincipalTypeAgent, principalType)
}

func TestChallengeLogger_publishesAssistantPrincipal(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_assistant_owner",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := MCPServer(MCPServerIDFrom("tool_assistant"), ProjectIDFrom(testProjectID)).Connect()
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, "user:user_assistant_owner", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeAssistant), message.GetPrincipalType())
}

func TestChallengeLogger_stampsRequestID(t *testing.T) {
	t.Parallel()

	reqID := "req_" + uuid.NewString()
	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_with_request",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{ReqID: reqID})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_req"}
	challengeRow{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeDeny,
		Reason:    authzrepo.ReasonNoGrants,
		Checks:    []Check{check},
		Focus:     &check,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, reqID, message.GetRequestId())
}

func TestChallengeLogger_publishesNestedAndExpandedFields(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_nested",
		AccountType:          "enterprise",
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	focus := Check{Scope: ScopeProjectRead, ResourceID: "proj_focus"}
	checks := []Check{
		focus,
		MCPServer(MCPServerIDFrom("tool_other"), ProjectIDFrom(testProjectID)).Connect(),
	}
	matches := []grantMatch{
		{
			Grant:    Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectWrite, Selector: NewSelector(ScopeProjectWrite, WildcardResource)},
			ViaCheck: Check{Scope: ScopeProjectWrite, ResourceID: "proj_focus"},
		},
	}

	challengeRow{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              checks,
		Focus:               &focus,
		Matches:             matches,
		EvaluatedGrantCount: 7,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Contains(t, message.GetExpandedScopes(), string(ScopeRoot))
	require.Contains(t, message.GetExpandedScopes(), string(ScopeProjectRead))
	require.Contains(t, message.GetExpandedScopes(), string(ScopeProjectWrite))
	require.Len(t, message.GetRequestedChecks(), 2)
	require.Equal(t, string(ScopeProjectRead), message.GetRequestedChecks()[0].GetScope())
	require.Equal(t, "proj_focus", message.GetRequestedChecks()[0].GetResourceId())
	require.Equal(t, string(ScopeMCPConnect), message.GetRequestedChecks()[1].GetScope())
	require.Equal(t, "tool_other", message.GetRequestedChecks()[1].GetResourceId())
	require.Len(t, message.GetMatchedGrants(), 1)
	require.Equal(t, "role:admin", message.GetMatchedGrants()[0].GetPrincipalUrn())
	require.Equal(t, string(ScopeProjectWrite), message.GetMatchedGrants()[0].GetScope())
	require.Equal(t, string(ScopeProjectWrite), message.GetMatchedGrants()[0].GetMatchedViaCheckScope())
	require.Equal(t, uint32(7), message.GetEvaluatedGrantCount())
}

func TestChallengeLogger_publishesFilterCounts(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_filter_counts",
		AccountType:          "enterprise",
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	focus := Check{Scope: ScopeProjectRead, ResourceID: "proj_filter"}

	challengeRow{
		Operation:            authzrepo.OperationFilter,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               []Check{focus},
		Focus:                &focus,
		FilterCandidateCount: 4,
		FilterAllowedCount:   1,
	}.write(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
	require.Equal(t, uint32(4), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
}

func TestWithOneChallengePerResource_recordsEachResourceOnce(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, granted := challengeScopeFixture(t)
	ctx = WithOneChallengePerResource(ctx)
	ungranted := uuid.NewString()

	// The server, then a tool on it: the second allow on the same server is
	// not recorded again.
	require.NoError(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(granted), ProjectIDFrom(testProjectID)).Connect()))
	require.NoError(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(granted), ProjectIDFrom(testProjectID)).ToolCall("search", "")))
	// A denial is recorded once however often it is checked.
	require.Error(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(ungranted), ProjectIDFrom(testProjectID)).Connect()))
	require.Error(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(ungranted), ProjectIDFrom(testProjectID)).Connect()))
	// A nested call keeps the request's record rather than starting over.
	require.NoError(t, engine.Require(WithOneChallengePerResource(ctx), MCPServer(MCPServerIDFrom(granted), ProjectIDFrom(testProjectID)).Connect()))

	require.Equal(t, [][2]string{{granted, "allow"}, {ungranted, "deny"}}, recordedChallengeOutcomes(t, conn))
}

func TestWithOneChallengePerResource_recordsADenialAfterAnAllow(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, granted := challengeScopeFixture(t)
	ctx = GrantsToContext(ctx, []Grant{{
		PrincipalUrn: "user:user_scope",
		Scope:        ScopeMCPConnect,
		Selector:     Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: granted, SelectorKeyDisposition: DispositionReadOnly},
	}})
	ctx = WithOneChallengePerResource(ctx)

	// Connecting is allowed, but this destructive tool is not: the refusal
	// is a real one and must still show.
	require.NoError(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(granted), ProjectIDFrom(testProjectID)).Connect()))
	require.Error(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(granted), ProjectIDFrom(testProjectID)).ToolCall("delete", DispositionDestructive)))

	require.Equal(t, [][2]string{{granted, "allow"}, {granted, "deny"}}, recordedChallengeOutcomes(t, conn))
}

// A Code Mode run shares one record across its callbacks, so a program that
// calls two refused tools on one server must still record both refusals,
// while refusing the same tool again records nothing new.
func TestWithOneChallengePerResource_recordsEachRefusedToolOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		calls []mcpToolCallDimensions
		want  int
	}{
		{
			name:  "two tools",
			calls: []mcpToolCallDimensions{{Tool: "delete", Disposition: DispositionDestructive, ProjectID: testProjectID}, {Tool: "drop", Disposition: DispositionDestructive, ProjectID: testProjectID}},
			want:  2,
		},
		{
			name:  "one tool twice",
			calls: []mcpToolCallDimensions{{Tool: "delete", Disposition: DispositionDestructive, ProjectID: testProjectID}, {Tool: "delete", Disposition: DispositionDestructive, ProjectID: testProjectID}},
			want:  1,
		},
		{
			name:  "one tool under two dispositions",
			calls: []mcpToolCallDimensions{{Tool: "delete", Disposition: DispositionDestructive, ProjectID: testProjectID}, {Tool: "delete", Disposition: DispositionOpenWorld, ProjectID: testProjectID}},
			want:  2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, engine, conn, granted := challengeScopeFixture(t)
			ctx = GrantsToContext(ctx, []Grant{{
				PrincipalUrn: "user:user_scope",
				Scope:        ScopeMCPConnect,
				Selector:     Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: granted, SelectorKeyDisposition: DispositionReadOnly},
			}})
			ctx = WithOneChallengePerResource(ctx)

			for _, call := range tc.calls {
				require.Error(t, engine.Require(ctx, mcpToolCallCheck(granted, call)))
			}

			want := make([][2]string, 0, tc.want)
			for range tc.want {
				want = append(want, [2]string{granted, "deny"})
			}
			require.Equal(t, want, recordedChallengeOutcomes(t, conn))
		})
	}
}

func TestWithOneChallengePerResource_batchesDoNotTakeAResourcesSlot(t *testing.T) {
	t.Parallel()

	ctx, engine, conn, _ := challengeScopeFixture(t)
	ctx = WithOneChallengePerResource(ctx)
	ungranted := uuid.NewString()
	tool := []Check{MCPServer(MCPServerIDFrom(ungranted), ProjectIDFrom(testProjectID)).ToolCall("search", "")}

	// Filtering the server's tools records a filter row even for one tool,
	// leaving the server's own denial to be recorded when a call names it.
	// Once that is recorded, a later filter about the server records nothing.
	_, err := allowedEach(ctx, engine, tool)
	require.NoError(t, err)
	require.Error(t, engine.Require(ctx, MCPServer(MCPServerIDFrom(ungranted), ProjectIDFrom(testProjectID)).Connect()))
	_, err = allowedEach(ctx, engine, tool)
	require.NoError(t, err)

	// A batch row names no single resource.
	require.Equal(t, [][2]string{{"", "deny"}, {ungranted, "deny"}}, recordedChallengeOutcomes(t, conn))
}

// Organization fixtures also enqueue bootstrap work. Challenge assertions only
// observe the authorization topic, without suppressing unrelated source events.
func listChallengeOutboxRows(t *testing.T, conn *pgxpool.Pool) []testrepo.ListPublishOutboxRowsRow {
	t.Helper()
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var challenges []testrepo.ListPublishOutboxRowsRow
	for _, row := range rows {
		if row.Topic == string(proto.MessageName(&authzv1.Challenge{})) {
			challenges = append(challenges, row)
		}
	}
	return challenges
}

// challengeScopeFixture is a session caller granted mcp:connect on one server
// only, with challenge logging on.
func challengeScopeFixture(t *testing.T) (context.Context, *Engine, *pgxpool.Pool, string) {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	sessionID := "session_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_scope",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	})
	granted := uuid.NewString()
	ctx = GrantsToContext(ctx, []Grant{
		{PrincipalUrn: "user:user_scope", Scope: ScopeMCPConnect, Selector: NewSelector(ScopeMCPConnect, granted)},
	})
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)
	return ctx, NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient()), conn, granted
}

// recordedChallengeOutcomes reads back the resource and outcome of every recorded
// check, in order.
func recordedChallengeOutcomes(t *testing.T, conn *pgxpool.Pool) [][2]string {
	t.Helper()
	var outcomes [][2]string
	for _, row := range listChallengeOutboxRows(t, conn) {
		message := &authzv1.Challenge{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		outcomes = append(outcomes, [2]string{message.GetResourceId(), message.GetOutcome()})
	}
	return outcomes
}
