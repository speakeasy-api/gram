package authz

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestChallengeLogger_skipsWithoutAuthContext(t *testing.T) {
	t.Parallel()

	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_1"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(t.Context(), conn, logger, staticChallengeLogging(true))

	row, err := conn.Query(t.Context(), `
		SELECT count() FROM authz_challenges WHERE resource_id = 'proj_1' AND organization_id = ''
	`)
	require.NoError(t, err)
	defer func() { _ = row.Close() }()

	var n uint64
	require.True(t, row.Next())
	require.NoError(t, row.Scan(&n))
	require.Equal(t, uint64(0), n)
}

func TestChallengeLogger_writesUserPrincipal(t *testing.T) {
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
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_user"}
	challengeLogger{
		Operation:           authzrepo.OperationRequire,
		Outcome:             authzrepo.OutcomeAllow,
		Reason:              authzrepo.ReasonGrantMatched,
		Checks:              []Check{check},
		Focus:               &check,
		Matches:             []grantMatch{{Grant: Grant{PrincipalUrn: "role:admin", Scope: ScopeProjectRead, Selector: NewSelector(ScopeProjectRead, WildcardResource)}, ViaCheck: check}},
		EvaluatedGrantCount: 1,
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT principal_urn, principal_type, user_id, user_external_id, user_email, session_id, role_slugs, operation, outcome, reason, scope, resource_kind, resource_id, evaluated_grant_count
			FROM authz_challenges
			WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			return false
		}
		var (
			urn, ptype, scope, rkind, rid, op, outcome, reason string
			userID, externalID, userEmail, sid                 *string
			roles                                              []string
			evalGrants                                         uint32
		)
		if err := rows.Scan(&urn, &ptype, &userID, &externalID, &userEmail, &sid, &roles, &op, &outcome, &reason, &scope, &rkind, &rid, &evalGrants); err != nil {
			return false
		}
		return urn == "user:user_principal" &&
			ptype == string(authzrepo.PrincipalTypeUser) &&
			userID != nil && *userID == "user_principal" &&
			externalID != nil && *externalID == "ext_principal" &&
			userEmail != nil && *userEmail == email &&
			sid != nil && *sid == sessionID &&
			len(roles) == 1 && roles[0] == "admin" &&
			op == string(authzrepo.OperationRequire) &&
			outcome == string(authzrepo.OutcomeAllow) &&
			reason == string(authzrepo.ReasonGrantMatched) &&
			scope == string(ScopeProjectRead) &&
			rkind == "project" &&
			rid == "proj_user" &&
			evalGrants == 1
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeLogger_writesAPIKeyPrincipal(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_owner",
		APIKeyID:             "key_abc",
		AccountType:          "enterprise",
	})
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_apikey"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT principal_urn, principal_type, api_key_id, user_id
			FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var (
			urn, ptype       string
			apiKeyID, userID *string
		)
		if err := rows.Scan(&urn, &ptype, &apiKeyID, &userID); err != nil {
			return false
		}
		return urn == "api_key:key_abc" &&
			ptype == string(authzrepo.PrincipalTypeAPIKey) &&
			apiKeyID != nil && *apiKeyID == "key_abc" &&
			userID != nil && *userID == "user_owner"
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeLogger_writesAssistantPrincipal(t *testing.T) {
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
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	check := Check{Scope: ScopeMCPConnect, ResourceID: "tool_assistant"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeAllow,
		Reason:    authzrepo.ReasonGrantMatched,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT principal_urn, principal_type FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var urn, ptype string
		if err := rows.Scan(&urn, &ptype); err != nil {
			return false
		}
		return urn == "user:user_assistant_owner" &&
			ptype == string(authzrepo.PrincipalTypeAssistant)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeLogger_stampsRequestID(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	reqID := "req_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_with_request",
		AccountType:          "enterprise",
	})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{ReqID: reqID})
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	check := Check{Scope: ScopeProjectRead, ResourceID: "proj_req"}
	challengeLogger{
		Operation: authzrepo.OperationRequire,
		Outcome:   authzrepo.OutcomeDeny,
		Reason:    authzrepo.ReasonNoGrants,
		Checks:    []Check{check},
		Focus:     &check,
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT request_id FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var got *string
		if err := rows.Scan(&got); err != nil {
			return false
		}
		return got != nil && *got == reqID
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeLogger_persistsNestedAndExpandedFields(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_nested",
		AccountType:          "enterprise",
	})
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

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
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT
				expanded_scopes,
				requested_checks.scope,
				requested_checks.resource_id,
				matched_grants.principal_urn,
				matched_grants.scope,
				matched_grants.matched_via_check_scope,
				evaluated_grant_count
			FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var (
			expanded, reqScopes, reqRIDs, mgURN, mgScope, mgVia []string
			evalGrants                                          uint32
		)
		if err := rows.Scan(&expanded, &reqScopes, &reqRIDs, &mgURN, &mgScope, &mgVia, &evalGrants); err != nil {
			return false
		}

		expandedSet := map[string]bool{}
		for _, s := range expanded {
			expandedSet[s] = true
		}
		if !expandedSet[string(ScopeRoot)] || !expandedSet[string(ScopeProjectRead)] || !expandedSet[string(ScopeProjectWrite)] {
			return false
		}

		return len(reqScopes) == 2 &&
			reqScopes[0] == string(ScopeProjectRead) && reqScopes[1] == string(ScopeMCPConnect) &&
			reqRIDs[0] == "proj_focus" && reqRIDs[1] == "tool_other" &&
			len(mgURN) == 1 && mgURN[0] == "role:admin" &&
			mgScope[0] == string(ScopeProjectWrite) &&
			mgVia[0] == string(ScopeProjectWrite) &&
			evalGrants == 7
	}, 5*time.Second, 100*time.Millisecond)
}

func TestChallengeLogger_persistsFilterCounts(t *testing.T) {
	t.Parallel()

	orgID := "org_" + uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		UserID:               "user_filter_counts",
		AccountType:          "enterprise",
	})
	conn, err := newClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)

	focus := Check{Scope: ScopeProjectRead, ResourceID: "proj_filter"}
	challengeLogger{
		Operation:            authzrepo.OperationFilter,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               []Check{focus},
		Focus:                &focus,
		FilterCandidateCount: 4,
		FilterAllowedCount:   1,
	}.Log(ctx, conn, logger, staticChallengeLogging(true))

	require.Eventually(t, func() bool {
		rows, err := conn.Query(t.Context(), `
			SELECT operation, filter_candidate_count, filter_allowed_count
			FROM authz_challenges WHERE organization_id = ?
		`, orgID)
		if err != nil {
			return false
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return false
		}
		var op string
		var candidate, allowed uint32
		if err := rows.Scan(&op, &candidate, &allowed); err != nil {
			return false
		}
		return op == string(authzrepo.OperationFilter) && candidate == 4 && allowed == 1
	}, 5*time.Second, 100*time.Millisecond)
}
