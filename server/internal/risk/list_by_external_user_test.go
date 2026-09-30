package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// seedExternalUserFindings writes one ClickHouse finding per external user id,
// each in its own chat, and returns the rows in input order.
func seedExternalUserFindings(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID, policyID string, externalUserIDs ...string) []chrepo.RiskFindingRow {
	t.Helper()

	at := time.Now().UTC().Add(-time.Hour)
	rows := make([]chrepo.RiskFindingRow, 0, len(externalUserIDs))
	for i, externalUserID := range externalUserIDs {
		chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, externalUserID)
		eventAt := at.Add(time.Duration(i) * time.Minute)
		rows = append(rows, chListFinding(t, projectID, orgID, chatID, msgID, policyID, eventAt, eventAt, "gitleaks", "aws-access-key-id", externalUserID, "AKIA**************LE", "", ""))
	}
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(t.Context(), rows))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	return rows
}

// The substring user_id filter routinely pulls in other people: "dev@acme.com"
// contains "dev@acme.co". The identity page needs exactly one subject, so
// external_user_ids matches whole ids, and takes a set because one person is
// recorded under several.
func TestListRiskResults_ExternalUserIDsMatchWholeIdentifiers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("External User Filter")})
	require.NoError(t, err)

	// The lookalike contains the subject's whole id as a prefix, so the
	// substring filter cannot tell them apart. The alias is the same person's
	// second identifier.
	rows := seedExternalUserFindings(t, ti, projectID, orgID, policy.ID, "dev@acme.co", "dev@acme.com", "personal@example.test")
	subject, lookalike, alias := rows[0], rows[1], rows[2]

	ids := func(result *gen.ListRiskResultsResult) []string {
		out := make([]string, 0, len(result.Results))
		for _, r := range result.Results {
			out = append(out, r.ID)
		}
		return out
	}

	substring, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
		UserID:   new("dev@acme.co"),
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{subject.ID.String(), lookalike.ID.String()}, ids(substring), "the substring filter also matches the lookalike")

	exact, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.co", "personal@example.test"},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{subject.ID.String(), alias.ID.String()}, ids(exact), "both of the subject's identifiers match, and the lookalike does not")

	subjectOnly, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.co"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{subject.ID.String()}, ids(subjectOnly))

	lookalikeOnly, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID:        &policy.ID,
		ExternalUserIds: []string{"dev@acme.com"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{lookalike.ID.String()}, ids(lookalikeOnly))
}

// An empty filter must not narrow the listing.
func TestListRiskResults_ExternalUserIDsEmptyIsUnnarrowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Unfiltered")})
	require.NoError(t, err)

	seedExternalUserFindings(t, ti, projectID, orgID, policy.ID, "one@example.test", "two@example.test")

	for _, externalUserIDs := range [][]string{nil, {}} {
		result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
			PolicyID:        &policy.ID,
			ExternalUserIds: externalUserIDs,
		})
		require.NoError(t, err)
		require.Len(t, result.Results, 2)
	}
}
