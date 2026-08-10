package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestChallengeLogger_skipsWithoutAuthContext(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(t.Context(), conn, testenv.NewLogger(t), staticChallengeLogging(true))

	count, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestChallengeLogger_skipsWhenImpersonating(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_admin",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetAdminOverrideInContext(ctx, orgID)
	conn := newTestDB(t)
	seedOrganization(t, ctx, conn, orgID)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_impersonated"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	count, err := testrepo.New(conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
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
	challengeLogger{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              []Check{check},
		Focus:               &check,
		Matches:             []grantMatch{{Grant: Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectRead, Selector: NewSelector(ScopeProjectRead, WildcardResource)}, ViaCheck: check}},
		EvaluatedGrantCount: 1,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
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
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, "api_key:key_abc", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeAPIKey), message.GetPrincipalType())
	require.Equal(t, "key_abc", message.GetApiKeyId())
	require.Equal(t, "user_owner", message.GetUserId())
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

	check := Check{Scope: ScopeMCPConnect, ResourceID: "tool_assistant"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
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
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeDeny,
		Reason:    authzrepo.ReasonNoGrants,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
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
		{Scope: ScopeMCPConnect, ResourceID: "tool_other"},
	}
	matches := []grantMatch{
		{
			Grant:    Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectWrite, Selector: NewSelector(ScopeProjectWrite, WildcardResource)},
			ViaCheck: Check{Scope: ScopeProjectWrite, ResourceID: "proj_focus"},
		},
	}

	challengeLogger{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              checks,
		Focus:               &focus,
		Matches:             matches,
		EvaluatedGrantCount: 7,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
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

	challengeLogger{
		Operation:            authzrepo.OperationFilter,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               []Check{focus},
		Focus:                &focus,
		FilterCandidateCount: 4,
		FilterAllowedCount:   1,
	}.Log(ctx, conn, testenv.NewLogger(t), staticChallengeLogging(true))

	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
	require.Equal(t, uint32(4), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
}
