package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestLogMandatoryDenialRecordsARefusedPersonalConnection(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{markerGrant})
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	authorization, err := engine.PrepareMandatory(ctx)
	require.NoError(t, err)
	check := personalClientCheck("user_456")
	decision, err := authorization.Decide(orgID, check)
	require.NoError(t, err)
	require.False(t, decision.Allowed)

	require.True(t, engine.LogMandatoryDenial(ctx, authorization, decision, check, "server_s"))

	rows := listChallengeOutboxRows(t, conn)
	require.Len(t, rows, 1)
	message := &authzv1.Challenge{}
	require.NoError(t, proto.Unmarshal(rows[0].Message, message))
	require.Equal(t, orgID, message.GetOrganizationId())
	require.Equal(t, "user:"+sessionUserID, message.GetPrincipalUrn())
	require.Equal(t, string(authzrepo.OperationRequire), message.GetOperation())
	require.Equal(t, string(authzrepo.OutcomeDeny), message.GetOutcome())
	require.Equal(t, string(authzrepo.ReasonConnectionOwnerMismatch), message.GetReason())
	require.Equal(t, string(ScopeMCPAuth), message.GetScope())
	require.Equal(t, ResourceKindMCPAuthClient, message.GetResourceKind())
	require.Equal(t, authClientA, message.GetResourceId())
	require.Equal(t, uint32(1), message.GetEvaluatedGrantCount())

	selector, err := SelectorFromRow([]byte(message.GetSelector()))
	require.NoError(t, err)
	require.Equal(t, Selector{
		SelectorKeyResourceKind: ResourceKindMCPAuthClient,
		SelectorKeyResourceID:   authClientA,
		SelectorKeyClientType:   AuthClientTypeRemoteSession,
		SelectorKeyProjectID:    authProjectP,
		ChallengeKeyMCPServerID: "server_s",
	}, selector, "the owner and upstream account stay out of the record")
	require.Len(t, message.GetRequestedChecks(), 1)
	require.Equal(t, message.GetSelector(), message.GetRequestedChecks()[0].GetSelector())
	require.NotContains(t, string(rows[0].Message), "user_456")
	require.NotContains(t, string(rows[0].Message), authUpstreamU)
}

func TestLogMandatoryDenialRecordsNothingForAnAllowedUse(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	conn := newTestDB(t)
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	ctx := GrantsToContext(enterpriseSessionCtxWithOrg(t, orgID), []Grant{markerGrant})
	seedOrganization(t, ctx, conn, orgID)
	engine := NewEngine(testenv.NewLogger(t), conn, staticChallengeLogging(true), workos.NewStubClient())

	authorization, err := engine.PrepareMandatory(ctx)
	require.NoError(t, err)
	check := personalClientCheck(sessionUserID)
	decision, err := authorization.Decide(orgID, check)
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	require.False(t, engine.LogMandatoryDenial(ctx, authorization, decision, check, "server_s"))

	require.Empty(t, listChallengeOutboxRows(t, conn))
}

func TestMandatoryChallengeReason(t *testing.T) {
	t.Parallel()

	user := MandatoryAuthorization{authorization: grantAuthorization{representedUser: sessionUserID}}
	cases := []struct {
		name       string
		reason     MandatoryDenyReason
		check      Check
		expected   authzrepo.Reason
		recordable bool
	}{
		{"another member's connection", MandatoryDenyReasonNotGranted, personalClientCheck("user_456"), authzrepo.ReasonConnectionOwnerMismatch, true},
		{"another member's connection without grants", MandatoryDenyReasonNoPolicy, personalClientCheck("user_456"), authzrepo.ReasonConnectionOwnerMismatch, true},
		{"own connection not granted", MandatoryDenyReasonNotGranted, personalClientCheck(sessionUserID), authzrepo.ReasonScopeUnsatisfied, true},
		{"shared client not granted", MandatoryDenyReasonNotGranted, sharedClientCheck(authProjectP), authzrepo.ReasonScopeUnsatisfied, true},
		{"shared client without grants", MandatoryDenyReasonNoPolicy, sharedClientCheck(authProjectP), authzrepo.ReasonNoGrants, true},
		{"blocked connection", MandatoryDenyReasonBlocked, personalClientCheck("user_456"), authzrepo.ReasonDenyGrant, true},
		{"outside key limits", MandatoryDenyReasonOutsideKey, sharedClientCheck(authProjectP), "", false},
		{"inactive api key", MandatoryDenyReasonAPIKeyInactive, sharedClientCheck(authProjectP), "", false},
		{"inactive credential", MandatoryDenyReasonInactiveCredential, personalClientCheck("user_456"), "", false},
		{"another organization", MandatoryDenyReasonOrganizationMismatch, sharedClientCheck(authProjectP), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision := MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: tc.reason}
			reason, recordable := mandatoryChallengeReason(user, decision, tc.check)
			require.Equal(t, tc.recordable, recordable)
			require.Equal(t, tc.expected, reason)
		})
	}
}
