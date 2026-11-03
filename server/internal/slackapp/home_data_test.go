package slackapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/slackapp/repo"
)

func homeRiskPolicy(t *testing.T, f homeFixture, score float64) riskrepo.RiskPolicy {
	t.Helper()
	row, err := riskrepo.New(f.db).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: f.projectID, OrganizationID: homeTestOrg, Name: "Test risk policy", PolicyType: "standard", Sources: []string{"gitleaks"}, PresidioEntities: []string{}, PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{}, Enabled: true, Action: "flag", AudienceType: "everyone", Score: pgtype.Float8{Float64: score, Valid: true},
	})
	require.NoError(t, err)
	return row
}

func homeFinding(t *testing.T, f homeFixture, policy riskrepo.RiskPolicy, source, rule string) uuid.UUID {
	t.Helper()
	q := riskrepo.New(f.db)
	chat, err := q.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{ProjectID: f.projectID, OrganizationID: homeTestOrg})
	require.NoError(t, err)
	message, err := q.CreateChatMessageForTest(t.Context(), riskrepo.CreateChatMessageForTestParams{ChatID: chat, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, Content: "fixture"})
	require.NoError(t, err)
	id := uuid.New()
	_, err = q.InsertRiskResults(t.Context(), []riskrepo.InsertRiskResultsParams{{ID: id, ProjectID: f.projectID, OrganizationID: homeTestOrg, RiskPolicyID: policy.ID, RiskPolicyVersion: policy.Version, ChatMessageID: uuid.NullUUID{UUID: message, Valid: true}, Found: true, Source: source, RuleID: pgtype.Text{String: rule, Valid: true}}})
	require.NoError(t, err)
	return id
}

func TestHomeRiskFiltersSeverityBeforeLimit(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	// Rounds to 7.0, matching the dashboard's displayed severity.
	high := homeRiskPolicy(t, f, 6.96)
	homeFinding(t, f, high, "gitleaks", "generic-api-key")
	low := homeRiskPolicy(t, f, 4)
	for range 8 {
		homeFinding(t, f, low, "gitleaks", "generic-api-key")
	}
	items, err := riskFindings(t.Context(), repo.New(f.db), homeTestOrg, homeLinks{})
	require.NoError(t, err)
	require.Len(t, items, 1, "newer low findings must not hide the older high event")
	require.Equal(t, "high", items[0].Severity)
}

func TestHomeRiskFallsBackToCategoryForDeletedPolicy(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	policy := homeRiskPolicy(t, f, 4)
	homeFinding(t, f, policy, "gitleaks", "generic-api-key")
	err := riskrepo.New(f.db).DeleteRiskPolicy(t.Context(), riskrepo.DeleteRiskPolicyParams{ID: policy.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	items, err := riskFindings(t.Context(), repo.New(f.db), homeTestOrg, homeLinks{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "high", items[0].Severity)
}

type noHomeFlags struct{}

func (noHomeFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func TestHomeClickRefusesDisabledFeature(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	events := f.events(t, &fakePoster{})
	events.home.flags = noHomeFlags{}
	handler := NewHomeInteractions(events, f.approvals, &recordingHandler{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, homeClickRequest(t, homeClickApprove, f.projectID.String()+":"+f.approvalID.String(), homeTestOrg))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, string(approval.StatusPending), f.status(t))
}

func TestHomeClickRefusesFormerMember(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	events := f.events(t, &fakePoster{})
	events.home.isMember = func(context.Context, string, string) (bool, error) { return false, nil }
	handler := NewHomeInteractions(events, f.approvals, &recordingHandler{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, homeClickRequest(t, homeClickApprove, f.projectID.String()+":"+f.approvalID.String(), homeTestOrg))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, string(approval.StatusPending), f.status(t))
}

func TestHomeClickStaleDecisionDoesNotOverride(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	events := f.events(t, &fakePoster{})
	handler := NewHomeInteractions(events, f.approvals, &recordingHandler{})
	value := f.projectID.String() + ":" + f.approvalID.String()
	for _, action := range []string{homeClickDeny, homeClickApprove} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, homeClickRequest(t, action, value, homeTestOrg))
		require.Equal(t, http.StatusOK, recorder.Code)
	}
	require.Equal(t, string(approval.StatusDenied), f.status(t))
}
