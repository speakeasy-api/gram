package risk_analysis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// This file holds the non-enforcing scan entrypoint for policy evals ("session
// replay"). It deliberately reuses the package's scan core (scanBatch) so an
// eval sees exactly what enforcement would — but returns the findings to the
// caller instead of writing them, never marks messages analyzed, and never
// appends to the outbox. See policy_eval.go for the write/cost-capture side.

// EvalConfigSnapshot is the frozen candidate-policy config stored in
// policy_eval_runs.config_snapshot for draft runs (no saved policy). It mirrors
// the evaluable fields of a saved risk_policies row so ScanBatchForEval can run
// a synthesized policy that was never persisted.
type EvalConfigSnapshot struct {
	PolicyType       string          `json:"policy_type"`
	Sources          []string        `json:"sources,omitempty"`
	PresidioEntities []string        `json:"presidio_entities,omitempty"`
	DisabledRules    []string        `json:"disabled_rules,omitempty"`
	CustomRuleIDs    []string        `json:"custom_rule_ids,omitempty"`
	MessageTypes     []string        `json:"message_types,omitempty"`
	ScopeInclude     string          `json:"scope_include,omitempty"`
	ScopeExempt      string          `json:"scope_exempt,omitempty"`
	Prompt           string          `json:"prompt,omitempty"`
	ModelConfig      json.RawMessage `json:"model_config,omitempty"`
}

// EvalSampleSpec is the JSON shape stored in policy_eval_runs.sample_definition.
// It captures how the sample was requested (mode + params) AND — once the run's
// sample-selection step resolves it — the pinned list of chat_message_ids the
// run scans, so a re-run hits exactly the same messages.
type EvalSampleSpec struct {
	Mode          string   `json:"mode"`
	LookbackDays  int      `json:"lookback_days,omitempty"`
	LastNSessions int      `json:"last_n_sessions,omitempty"`
	MaxMessages   int      `json:"max_messages"`
	MessageIDs    []string `json:"message_ids,omitempty"`
}

// EvalMessageFindings pairs a scanned message with the findings the policy
// produced for it. The MessageID is carried explicitly because the scan may
// filter messages (by message type), so positional alignment with the input
// id list is not guaranteed.
type EvalMessageFindings struct {
	MessageID uuid.UUID
	Findings  []Finding
}

// ScanBatchForEval scans the batch for the policy-eval path. It runs the same
// scanners, scope, and disabled-rule filtering as enforcement (via scanBatch),
// but it never writes results, marks messages analyzed, or appends to the
// outbox — the caller owns persistence (WriteEvalFindings) and cost capture
// (set args.JudgeObserve). Unlike Do it does NOT skip a disabled policy: the
// whole point of an eval is to try a not-yet-enabled (or unsaved) policy, so
// the caller supplies the policy config directly (a saved row or one built by
// SyntheticPolicyForSnapshot).
func (a *AnalyzeBatch) ScanBatchForEval(ctx context.Context, args AnalyzeBatchArgs, policy repo.RiskPolicy) ([]EvalMessageFindings, error) {
	if len(args.MessageIDs) == 0 {
		return nil, nil
	}

	rows, err := a.fetchContent(ctx, args)
	if err != nil {
		return nil, err
	}
	rows = filterMessagesByMessageTypes(rows, args.MessageTypes)
	messages := newBatchMessages(ctx, a.logger, rows)
	if len(messages) == 0 {
		return nil, nil
	}

	findings, err := a.scanBatch(ctx, args, policy, messages)
	if err != nil {
		return nil, err
	}

	out := make([]EvalMessageFindings, 0, len(messages))
	for i, m := range messages {
		var fs []Finding
		if i < len(findings) {
			fs = findings[i]
		}
		out = append(out, EvalMessageFindings{MessageID: m.ID, Findings: fs})
	}
	return out, nil
}

// SyntheticPolicyForSnapshot builds an in-memory repo.RiskPolicy from a frozen
// draft config so ScanBatchForEval can evaluate a candidate that was never
// saved. Only the fields the scan core reads are populated; the row is never
// persisted. Detection-source selection (Sources/PresidioEntities) travels on
// the AnalyzeBatchArgs, not the policy row, so it is not set here.
func SyntheticPolicyForSnapshot(projectID uuid.UUID, snap EvalConfigSnapshot) repo.RiskPolicy {
	policyType := snap.PolicyType
	if policyType == "" {
		policyType = PolicyTypeStandard
	}
	zeroText := pgtype.Text{String: "", Valid: false}
	zeroTime := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	return repo.RiskPolicy{
		ID:                   uuid.Nil,
		ProjectID:            projectID,
		OrganizationID:       "",
		Enabled:              true,
		Name:                 "",
		PolicyType:           policyType,
		Sources:              snap.Sources,
		PresidioEntities:     snap.PresidioEntities,
		PromptInjectionRules: nil,
		DisabledRules:        snap.DisabledRules,
		CustomRuleIds:        snap.CustomRuleIDs,
		MessageTypes:         snap.MessageTypes,
		ScopeInclude:         pgtype.Text{String: snap.ScopeInclude, Valid: snap.ScopeInclude != ""},
		ScopeExempt:          pgtype.Text{String: snap.ScopeExempt, Valid: snap.ScopeExempt != ""},
		Action:               "",
		AudienceType:         "",
		AutoName:             false,
		UserMessage:          zeroText,
		Prompt:               pgtype.Text{String: snap.Prompt, Valid: snap.Prompt != ""},
		ModelConfig:          snap.ModelConfig,
		Version:              0,
		CreatedAt:            zeroTime,
		UpdatedAt:            zeroTime,
		DeletedAt:            zeroTime,
		Deleted:              false,
	}
}
