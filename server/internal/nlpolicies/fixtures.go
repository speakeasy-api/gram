package nlpolicies

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
)

var (
	fixturePolicy1ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fixturePolicy2ID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fixturePolicy3ID = uuid.MustParse("33333333-3333-3333-3333-333333333333")
)

// fixturePolicies is the canned policy list every org sees in PR 1.
// Replaced by DB-backed queries in PR 3.
func fixturePolicies() []*types.NLPolicy {
	now := time.Now().UTC().Format(time.RFC3339)
	return []*types.NLPolicy{
		{
			ID:           fixturePolicy1ID.String(),
			Name:         "No deletes against prod",
			Description:  new("Blocks deletes targeting production-tagged MCPs."),
			NlPrompt:     "Refuse any tool call whose name or description indicates a destructive operation (delete, drop, truncate, purge) when the target MCP slug is tagged \"production\". Allow read operations.",
			ScopePerCall: true,
			ScopeSession: false,
			Mode:         "audit",
			FailMode:     "fail_open",
			StaticRules:  "[]",
			Version:      1,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           fixturePolicy2ID.String(),
			Name:         "Block exfiltration",
			Description:  new("Detects multi-call exfiltration patterns across a session."),
			NlPrompt:     "Watch the session for patterns where the agent reads sensitive customer data and then sends it to an external destination (Slack, email, webhook). Flag the session for quarantine.",
			ScopePerCall: false,
			ScopeSession: true,
			Mode:         "enforce",
			FailMode:     "fail_open",
			StaticRules:  "[]",
			Version:      3,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           fixturePolicy3ID.String(),
			Name:         "MCP allowlist",
			Description:  new("Static deny on external MCPs not on the platform."),
			NlPrompt:     "Refuse any call to an external-MCP that is not on the platform allowlist.",
			ScopePerCall: true,
			ScopeSession: false,
			Mode:         "disabled",
			FailMode:     "fail_open",
			StaticRules:  `[{"action":"deny","match":{"target_mcp_kind":"external-mcp"}}]`,
			Version:      1,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
}

func fixtureDecisions() []*types.NLPolicyDecision {
	now := time.Now().UTC()
	out := make([]*types.NLPolicyDecision, 0, 50)
	for i := range 50 {
		ts := now.Add(time.Duration(-i) * time.Minute).Format(time.RFC3339)
		decision, decidedBy, reason, enforced := "ALLOW", "llm_judge", "no policy violation", false
		switch i % 7 {
		case 1:
			decision, decidedBy, reason, enforced = "BLOCK", "llm_judge", "destructive operation against production", false
		case 3:
			decision, decidedBy, reason, enforced = "JUDGE_ERROR", "fail_mode", "openrouter timeout (4500ms)", false
		case 5:
			decision, decidedBy, reason, enforced = "BLOCK", "static_rule", "matched deny rule: external-mcp", true
		}
		out = append(out, &types.NLPolicyDecision{
			ID:              uuid.New().String(),
			NlPolicyID:      fixturePolicy1ID.String(),
			NlPolicyVersion: 1,
			SessionID:       new("ses_" + uuid.NewString()[:8]),
			ToolUrn:         "tools:http:acme:" + []string{"list_invoices", "delete_invoice", "create_invoice", "get_customer", "delete_customer"}[i%5],
			Decision:        decision,
			DecidedBy:       decidedBy,
			Reason:          new(reason),
			Mode:            "audit",
			Enforced:        enforced,
			JudgeLatencyMs:  new(120 + i*3),
			CreatedAt:       ts,
		})
	}
	return out
}

func fixtureSessionVerdicts() []*types.NLPolicySessionVerdict {
	now := time.Now().UTC()
	q1 := now.Add(-2 * time.Hour).Format(time.RFC3339)
	q2 := now.Add(-26 * time.Hour).Format(time.RFC3339)
	reason1 := "session pattern matches exfiltration: read customer data + slack post within 4 calls"
	reason2 := "session pattern matches exfiltration: bulk read + email send"
	return []*types.NLPolicySessionVerdict{
		{
			ID:              uuid.New().String(),
			SessionID:       "ses_8f3a2b14",
			NlPolicyID:      fixturePolicy2ID.String(),
			NlPolicyVersion: 3,
			Verdict:         "QUARANTINED",
			Reason:          &reason1,
			QuarantinedAt:   &q1,
			CreatedAt:       q1,
		},
		{
			ID:              uuid.New().String(),
			SessionID:       "ses_b1c4e0d7",
			NlPolicyID:      fixturePolicy2ID.String(),
			NlPolicyVersion: 3,
			Verdict:         "QUARANTINED",
			Reason:          &reason2,
			QuarantinedAt:   &q2,
			CreatedAt:       q2,
		},
	}
}

func fixtureReplayRun() *types.NLPolicyReplayRun {
	now := time.Now().UTC()
	startedAt := now.Add(-5 * time.Minute).Format(time.RFC3339)
	completedAt := now.Add(-5*time.Minute + 18*time.Second).Format(time.RFC3339)
	return &types.NLPolicyReplayRun{
		ID:              "r3a8f2",
		NlPolicyID:      fixturePolicy1ID.String(),
		NlPolicyVersion: 1,
		Status:          "completed",
		Counts:          new(`{"would_block":14,"would_allow":84,"judge_error":2}`),
		SampleFilter:    `{"window":"7d","sample_size":100,"scope":"per_call"}`,
		StartedAt:       startedAt,
		CompletedAt:     &completedAt,
	}
}
