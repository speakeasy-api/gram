package risk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// fakeRiskService only implements the methods a test exercises; the embedded
// interface makes the rest panic rather than silently returning zero values.
type fakeRiskService struct {
	RiskService

	exclusions []*types.RiskExclusion
	created    []*risk.CreateRiskExclusionPayload
	agentLimit *int
}

func (f *fakeRiskService) ListRiskExclusions(ctx context.Context, payload *risk.ListRiskExclusionsPayload) (*risk.ListRiskExclusionsResult, error) {
	return &risk.ListRiskExclusionsResult{Exclusions: f.exclusions}, nil
}

func (f *fakeRiskService) CreateRiskExclusion(ctx context.Context, payload *risk.CreateRiskExclusionPayload) (*types.RiskExclusion, error) {
	f.created = append(f.created, payload)
	return &types.RiskExclusion{
		ID:           "0192bd2a-0000-7000-8000-00000000000f",
		ProjectID:    "0192bd2a-0000-7000-8000-000000000001",
		RiskPolicyID: payload.RiskPolicyID,
		MatchType:    payload.MatchType,
		MatchValue:   payload.MatchValue,
		RuleIDFilter: payload.RuleIDFilter,
		SourceFilter: payload.SourceFilter,
		Enabled:      payload.Enabled,
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}, nil
}

func (f *fakeRiskService) ListRiskResultsForAgent(ctx context.Context, payload *risk.ListRiskResultsForAgentPayload) (*risk.ListRiskResultsForAgentResult, error) {
	f.agentLimit = payload.Limit
	return &risk.ListRiskResultsForAgentResult{Results: nil, TotalCount: 0, NextCursor: nil}, nil
}

func existingExclusion() *types.RiskExclusion {
	return &types.RiskExclusion{
		ID:           "0192bd2a-0000-7000-8000-000000000000",
		ProjectID:    "0192bd2a-0000-7000-8000-000000000001",
		RiskPolicyID: nil,
		MatchType:    "exact",
		MatchValue:   "AKIAIOSFODNN7EXAMPLE",
		RuleIDFilter: "secret.aws_access_key",
		SourceFilter: "gitleaks",
		Enabled:      true,
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}
}

func callCreate(t *testing.T, svc RiskService, payload string) exclusionView {
	t.Helper()

	var out bytes.Buffer
	err := NewCreateRiskExclusionTool(svc).Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out)
	require.NoError(t, err)

	var view exclusionView
	require.NoError(t, json.Unmarshal(out.Bytes(), &view))

	return view
}

// The listing fingerprints exact/regex match values, so the model cannot dedupe
// on its own — an equivalent exclusion must be reused server-side instead.
func TestCreateRiskExclusionReusesEquivalent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
	}{
		{name: "global via omitted policy id", payload: `{"match_type":"exact","match_value":"AKIAIOSFODNN7EXAMPLE","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks"}`},
		// The service reads "" as global, so the tool must not treat it as a
		// separate scope and create a second row.
		{name: "global via empty policy id", payload: `{"match_type":"exact","match_value":"AKIAIOSFODNN7EXAMPLE","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks","risk_policy_id":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeRiskService{exclusions: []*types.RiskExclusion{existingExclusion()}, created: nil, agentLimit: nil}

			view := callCreate(t, svc, tt.payload)

			require.Empty(t, svc.created, "an equivalent exclusion already exists")
			require.Equal(t, "0192bd2a-0000-7000-8000-000000000000", view.ID)
			require.NotContains(t, view.MatchValue, "AKIAIOSFODNN7EXAMPLE")
		})
	}
}

// An empty risk_policy_id means global to the service, so it must reach it as
// nil rather than as a distinct scope the equivalence check cannot match.
func TestCreateRiskExclusionNormalizesEmptyPolicyID(t *testing.T) {
	t.Parallel()

	svc := &fakeRiskService{exclusions: nil, created: nil, agentLimit: nil}

	callCreate(t, svc, `{"match_type":"exact","match_value":"secret","risk_policy_id":""}`)

	require.Len(t, svc.created, 1)
	require.Nil(t, svc.created[0].RiskPolicyID)
}

func TestCreateRiskExclusionCreatesWhenNotEquivalent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
	}{
		{name: "different match value", payload: `{"match_type":"exact","match_value":"other","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks"}`},
		{name: "different match type", payload: `{"match_type":"regex","match_value":"AKIAIOSFODNN7EXAMPLE","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks"}`},
		{name: "different rule filter", payload: `{"match_type":"exact","match_value":"AKIAIOSFODNN7EXAMPLE","source_filter":"gitleaks"}`},
		{name: "different source filter", payload: `{"match_type":"exact","match_value":"AKIAIOSFODNN7EXAMPLE","rule_id_filter":"secret.aws_access_key"}`},
		{name: "policy bound rather than global", payload: `{"match_type":"exact","match_value":"AKIAIOSFODNN7EXAMPLE","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks","risk_policy_id":"0192bd2a-0000-7000-8000-000000000002"}`},
		// A disabled row does not suppress anything, so reusing it for an enable
		// request would report success while the findings stay flagged.
		{name: "enabling a disabled exclusion", payload: `{"match_type":"exact","match_value":"disabled-draft","rule_id_filter":"secret.aws_access_key","source_filter":"gitleaks"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			disabled := existingExclusion()
			disabled.ID = "0192bd2a-0000-7000-8000-000000000003"
			disabled.MatchValue = "disabled-draft"
			disabled.Enabled = false

			svc := &fakeRiskService{exclusions: []*types.RiskExclusion{existingExclusion(), disabled}, created: nil, agentLimit: nil}
			callCreate(t, svc, tt.payload)

			require.Len(t, svc.created, 1)
		})
	}
}

// DecodeInput is a plain unmarshal, so the schema's range on limit is advisory:
// the handler has to enforce the token budget itself.
func TestListRiskResultsForAgentClampsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		payload string
		want    int
	}{
		{payload: `{}`, want: agentResultsDefaultLimit},
		{payload: `{"limit":10}`, want: 10},
		{payload: `{"limit":5000}`, want: agentResultsMaxLimit},
		{payload: `{"limit":0}`, want: 1},
		{payload: `{"limit":-3}`, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.payload, func(t *testing.T) {
			t.Parallel()

			svc := &fakeRiskService{exclusions: nil, created: nil, agentLimit: nil}

			var out bytes.Buffer
			err := NewListRiskResultsForAgentTool(svc).Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(tt.payload), &out)
			require.NoError(t, err)

			require.NotNil(t, svc.agentLimit)
			require.Equal(t, tt.want, *svc.agentLimit)
		})
	}
}
