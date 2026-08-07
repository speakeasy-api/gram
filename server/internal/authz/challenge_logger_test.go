package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func capturingChallengePublisher(t *testing.T) (*gcp.MockPublisher[*authzv1.Challenge], *[]*authzv1.Challenge) {
	t.Helper()

	published := make([]*authzv1.Challenge, 0, 1)
	publisher := gcp.NewMockPublisher[*authzv1.Challenge]()
	publisher.
		On("Publish", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			message, ok := args.Get(1).(*authzv1.Challenge)
			require.True(t, ok)
			published = append(published, message)
		}).
		Return(gcp.NewSuccessPublishResult()).
		Once()
	t.Cleanup(func() {
		publisher.AssertExpectations(t)
	})

	return publisher, &published
}

func TestChallengeLogger_skipsWithoutAuthContext(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*authzv1.Challenge]()
	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(t.Context(), publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
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
	publisher := gcp.NewMockPublisher[*authzv1.Challenge]()

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_impersonated"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
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
	publisher, published := capturingChallengePublisher(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_user"}
	challengeLogger{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              []Check{check},
		Focus:               &check,
		Matches:             []grantMatch{{Grant: Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectRead, Selector: NewSelector(ScopeProjectRead, WildcardResource)}, ViaCheck: check}},
		EvaluatedGrantCount: 1,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	message := (*published)[0]
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
	publisher, published := capturingChallengePublisher(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_apikey"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	message := (*published)[0]
	require.Equal(t, "api_key:key_abc", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeAPIKey), message.GetPrincipalType())
	require.Equal(t, "key_abc", message.GetApiKeyId())
	require.Equal(t, "user_owner", message.GetUserId())
}

func TestChallengeLogger_publishesAssistantPrincipal(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_" + uuid.NewString(),
		UserID:               "user_assistant_owner",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})
	publisher, published := capturingChallengePublisher(t)

	check := Check{Scope: ScopeMCPConnect, ResourceID: "tool_assistant"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	message := (*published)[0]
	require.Equal(t, "user:user_assistant_owner", message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.PrincipalTypeAssistant), message.GetPrincipalType())
}

func TestChallengeLogger_stampsRequestID(t *testing.T) {
	t.Parallel()

	reqID := "req_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_" + uuid.NewString(),
		UserID:               "user_with_request",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{ReqID: reqID})
	publisher, published := capturingChallengePublisher(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_req"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeDeny,
		Reason:    authzrepo.ReasonNoGrants,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	require.Equal(t, reqID, (*published)[0].GetRequestId())
}

func TestChallengeLogger_publishesNestedAndExpandedFields(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_" + uuid.NewString(),
		UserID:               "user_nested",
		AccountType:          "enterprise",
	})
	publisher, published := capturingChallengePublisher(t)
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
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	message := (*published)[0]
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

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_" + uuid.NewString(),
		UserID:               "user_filter_counts",
		AccountType:          "enterprise",
	})
	publisher, published := capturingChallengePublisher(t)
	focus := Check{Scope: ScopeProjectRead, ResourceID: "proj_filter"}

	challengeLogger{
		Operation:            authzrepo.OperationFilter,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               []Check{focus},
		Focus:                &focus,
		FilterCandidateCount: 4,
		FilterAllowedCount:   1,
	}.Log(ctx, publisher, testenv.NewLogger(t), staticChallengeLogging(true))

	require.Len(t, *published, 1)
	message := (*published)[0]
	require.Equal(t, string(authzrepo.OperationFilter), message.GetOperation())
	require.Equal(t, uint32(4), message.GetFilterCandidateCount())
	require.Equal(t, uint32(1), message.GetFilterAllowedCount())
}
