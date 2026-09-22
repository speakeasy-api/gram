package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestListRiskResults_ClickHouseShadowRowsHidden pins the shadow
// engine-comparison contract on the findings store. Under the shadow risk
// engine mode the LLM analyzer's findings are persisted with shadow = 1 next
// to the legacy engines' rows for the same message and policy, so the
// comparison query can join the two, but neither the Risk Events listing,
// its total count, nor the Dismissed listing ever serves a shadow row, while
// the legacy row is unaffected.
func TestListRiskResults_ClickHouseShadowRowsHidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Shadow")})
	require.NoError(t, err)

	// Relative times: the table's 90-day created_at TTL expires hardcoded
	// dates at insert once the calendar catches up.
	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	legacy := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(time.Hour), base, "gitleaks", "secret.github_pat", "alice@example.com", "AKIA****XY", "fp-legacy", "")

	// The model's verdict for the same message and policy. Deterministic
	// finding ids differ by source, so the two rows coexist.
	shadow := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(time.Hour), base, llmanalyzer.Source, "secrets_leak", "alice@example.com", "", "fp-shadow", "")
	shadow.Shadow = true

	// A shadow row an exclusion rule suppressed at ingest: hidden from the
	// Dismissed listing just like the live one is hidden from Risk Events.
	excludedAt := base.Add(2 * time.Hour)
	exclusionID := uuid.Must(uuid.NewV7())
	shadowSuppressed := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(time.Hour), base, llmanalyzer.Source, "pii_exposure", "alice@example.com", "", "fp-shadow-suppressed", "")
	shadowSuppressed.Shadow = true
	shadowSuppressed.ExcludedAt = &excludedAt
	shadowSuppressed.ExclusionID = &exclusionID
	shadowSuppressed.ExcludedReason = chrepo.ExcludedReasonRule

	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{legacy, shadow, shadowSuppressed}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Stored: every row landed, and only the model's carry the marker. Raw
	// SQL on purpose: ClickHouse fixture reads are exempt from the
	// no-raw-SQL test rule, and no read path serves shadow rows.
	rows, err := ti.chConn.Query(ctx, `SELECT id, shadow FROM risk_findings WHERE organization_id = ? AND chat_message_id = ?`, orgID, msgID.String())
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	stored := map[uuid.UUID]uint8{}
	for rows.Next() {
		var (
			id     uuid.UUID
			marker uint8
		)
		require.NoError(t, rows.Scan(&id, &marker))
		stored[id] = marker
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[uuid.UUID]uint8{
		legacy.ID:           0,
		shadow.ID:           1,
		shadowSuppressed.ID: 1,
	}, stored)

	// Risk Events: only the legacy row, and the count agrees.
	page, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	require.Equal(t, legacy.ID.String(), page.Results[0].ID)
	require.Equal(t, int64(1), page.TotalCount)

	// Dismissed: the suppressed shadow row never surfaces either.
	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, dismissed.Results)
	require.Zero(t, dismissed.TotalCount)
}
