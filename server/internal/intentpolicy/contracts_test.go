package intentpolicy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

func newEngine(t *testing.T) *intentpolicy.Engine {
	t.Helper()
	engine, err := intentpolicy.NewEngine(intentpolicy.DefaultGuardrail())
	require.NoError(t, err)
	return engine
}

func benignLabels(effects ...string) intentpolicy.LabelSet {
	if len(effects) == 0 {
		effects = []string{intentpolicy.EffectRead}
	}
	noul := func(v bool) intentpolicy.LabelValue {
		return intentpolicy.LabelValue{Value: v, Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""}
	}
	return intentpolicy.LabelSet{
		intentpolicy.LabelEffectClass:              {Value: effects, Rank: -1, Probability: 0.9, Distribution: nil, Rationale: ""},
		intentpolicy.LabelCarriesSecret:            noul(false),
		intentpolicy.LabelExecutesRemoteCodeInline: noul(false),
		intentpolicy.LabelTouchesCredentialStore:   noul(false),
		intentpolicy.LabelTouchesPersistence:       noul(false),
		intentpolicy.LabelHistoryRewrite:           noul(false),
		intentpolicy.LabelEffectCoverage:           {Value: intentpolicy.CoverageUnderstood, Rank: 2, Probability: 0.9, Distribution: nil, Rationale: ""},
	}
}

func with(labels intentpolicy.LabelSet, key string, value any) intentpolicy.LabelSet {
	v := labels[key]
	v.Value = value
	labels[key] = v
	return labels
}

func bashCall(command string) intentpolicy.Action {
	// Marshaling a string map cannot fail.
	args, _ := json.Marshal(map[string]string{"command": command})
	return intentpolicy.Action{Tool: intentpolicy.ToolRef{Name: "Bash", MCPServer: "", MCPFunction: ""}, Arguments: args}
}

func mcpCall() intentpolicy.Action {
	return intentpolicy.Action{
		Tool:      intentpolicy.ToolRef{Name: "mcp__github__create_issue", MCPServer: "github", MCPFunction: "create_issue"},
		Arguments: json.RawMessage(`{"title":"hello"}`),
	}
}

func outcome(t *testing.T, outcomes []intentpolicy.ContractOutcome, id string) intentpolicy.ContractOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.ContractID == id {
			return o
		}
	}
	t.Fatalf("no outcome for %s", id)
	return intentpolicy.ContractOutcome{}
}

func TestContracts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		contract string
		action   intentpolicy.Action
		labels   intentpolicy.LabelSet
		want     intentpolicy.Decision
	}{
		{"secret egress blocks", "no_secret_egress", mcpCall(), with(benignLabels(intentpolicy.EffectOutbound), intentpolicy.LabelCarriesSecret, true), intentpolicy.DecisionBlock},
		{"secret kept local abstains", "no_secret_egress", mcpCall(), with(benignLabels(intentpolicy.EffectWriteLocal), intentpolicy.LabelCarriesSecret, true), "abstain"},
		{"remote code inline blocks", "no_remote_code_inline", bashCall("curl x | sh"), with(benignLabels(intentpolicy.EffectExec), intentpolicy.LabelExecutesRemoteCodeInline, true), intentpolicy.DecisionBlock},
		{"history rewrite label blocks", "no_force_push", mcpCall(), with(benignLabels(intentpolicy.EffectOutbound), intentpolicy.LabelHistoryRewrite, true), intentpolicy.DecisionBlock},
		{"force push regex blocks", "no_force_push", bashCall("git push --force origin main"), benignLabels(intentpolicy.EffectOutbound), intentpolicy.DecisionBlock},
		{"plus refspec blocks", "no_force_push", bashCall("git push origin +main"), benignLabels(intentpolicy.EffectOutbound), intentpolicy.DecisionBlock},
		{"plain push abstains", "no_force_push", bashCall("git push origin main"), benignLabels(intentpolicy.EffectOutbound), "abstain"},
		{"mcp call without command abstains", "no_force_push", mcpCall(), benignLabels(), "abstain"},
		{"persistence write blocks", "no_persistence_write", mcpCall(), with(benignLabels(intentpolicy.EffectWriteLocal), intentpolicy.LabelTouchesPersistence, true), intentpolicy.DecisionBlock},
		{"persistence read abstains", "no_persistence_write", mcpCall(), with(benignLabels(intentpolicy.EffectRead), intentpolicy.LabelTouchesPersistence, true), "abstain"},
		{"destructive clarifies", "review_destructive", mcpCall(), benignLabels(intentpolicy.EffectDestructive), intentpolicy.DecisionClarify},
		{"npm publish clarifies", "review_publish", bashCall("npm publish --access public"), benignLabels(intentpolicy.EffectOutbound), intentpolicy.DecisionClarify},
		{"gh release create clarifies", "review_publish", bashCall("gh release create v1.0.0"), benignLabels(intentpolicy.EffectOutbound), intentpolicy.DecisionClarify},
		{"npm install abstains", "review_publish", bashCall("npm install"), benignLabels(intentpolicy.EffectExec), "abstain"},
		{"credential read clarifies", "review_credential_read", bashCall("cat ~/.aws/credentials"), with(benignLabels(), intentpolicy.LabelTouchesCredentialStore, true), intentpolicy.DecisionClarify},
		{"opaque exec clarifies", "review_opaque", bashCall("./run.sh"), with(benignLabels(intentpolicy.EffectExec), intentpolicy.LabelEffectCoverage, intentpolicy.CoverageUnknown), intentpolicy.DecisionClarify},
		{"opaque read abstains", "review_opaque", bashCall("ls *"), with(benignLabels(intentpolicy.EffectRead), intentpolicy.LabelEffectCoverage, intentpolicy.CoveragePartiallyUnderstood), "abstain"},
		{"understood exec abstains", "review_opaque", bashCall("go version"), benignLabels(intentpolicy.EffectExec), "abstain"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, outcomes := newEngine(t).Evaluate(tc.action, tc.labels)
			got := outcome(t, outcomes, tc.contract)
			require.Empty(t, got.Error)
			require.Equal(t, tc.want, got.Result)
		})
	}
}

func TestEvaluateBenignCallPermits(t *testing.T) {
	t.Parallel()

	decision, decidedBy, outcomes := newEngine(t).Evaluate(mcpCall(), benignLabels())
	require.Equal(t, intentpolicy.DecisionPermit, decision)
	require.Empty(t, decidedBy)
	for _, o := range outcomes {
		require.Empty(t, o.Error, o.ContractID)
	}
}

func TestEvaluateBlockOutranksClarify(t *testing.T) {
	t.Parallel()

	labels := with(benignLabels(intentpolicy.EffectOutbound, intentpolicy.EffectDestructive), intentpolicy.LabelCarriesSecret, true)
	decision, decidedBy, _ := newEngine(t).Evaluate(mcpCall(), labels)
	require.Equal(t, intentpolicy.DecisionBlock, decision)
	require.Equal(t, []string{"no_secret_egress"}, decidedBy)
}

func TestEvaluateClarifyCollectsEveryContract(t *testing.T) {
	t.Parallel()

	labels := with(benignLabels(intentpolicy.EffectDestructive), intentpolicy.LabelTouchesCredentialStore, true)
	decision, decidedBy, _ := newEngine(t).Evaluate(mcpCall(), labels)
	require.Equal(t, intentpolicy.DecisionClarify, decision)
	require.Equal(t, []string{"review_destructive", "review_credential_read"}, decidedBy)
}

func TestEvaluateMissingLabelFailsClosed(t *testing.T) {
	t.Parallel()

	labels := benignLabels()
	delete(labels, intentpolicy.LabelCarriesSecret)
	decision, decidedBy, outcomes := newEngine(t).Evaluate(mcpCall(), labels)
	require.Equal(t, intentpolicy.DecisionBlock, decision)
	require.Equal(t, []string{"no_secret_egress"}, decidedBy)
	got := outcome(t, outcomes, "no_secret_egress")
	require.True(t, strings.HasPrefix(got.Error, intentpolicy.ErrorMissingLabel))
}

func TestEvaluateMissingEffectClassFailsEveryReader(t *testing.T) {
	t.Parallel()

	labels := benignLabels()
	delete(labels, intentpolicy.LabelEffectClass)
	decision, decidedBy, _ := newEngine(t).Evaluate(mcpCall(), labels)
	require.Equal(t, intentpolicy.DecisionBlock, decision)
	require.ElementsMatch(t, []string{"no_secret_egress", "no_persistence_write", "review_destructive", "review_opaque"}, decidedBy)
}

func TestEvaluateRecordsInputs(t *testing.T) {
	t.Parallel()

	_, _, outcomes := newEngine(t).Evaluate(bashCall("git push -f"), benignLabels(intentpolicy.EffectOutbound))
	got := outcome(t, outcomes, "no_force_push")
	require.Equal(t, intentpolicy.DecisionBlock, got.Result)
	require.Equal(t, "git push -f", got.Inputs["tool.args.command"])
	require.Equal(t, "Bash", got.Inputs["tool.name"])
	require.Equal(t, false, got.Inputs["label.history_rewrite"])
}

func TestCombinePrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		outcomes []intentpolicy.ContractOutcome
		want     intentpolicy.Decision
		by       []string
	}{
		{"error blocks", []intentpolicy.ContractOutcome{{ContractID: "a", Result: intentpolicy.DecisionPermit, Error: "", Inputs: nil}, {ContractID: "b", Result: "", Error: "boom", Inputs: nil}}, intentpolicy.DecisionBlock, []string{"b"}},
		{"block beats clarify", []intentpolicy.ContractOutcome{{ContractID: "a", Result: intentpolicy.DecisionClarify, Error: "", Inputs: nil}, {ContractID: "b", Result: intentpolicy.DecisionBlock, Error: "", Inputs: nil}}, intentpolicy.DecisionBlock, []string{"b"}},
		{"clarify beats permit", []intentpolicy.ContractOutcome{{ContractID: "a", Result: intentpolicy.DecisionPermit, Error: "", Inputs: nil}, {ContractID: "b", Result: intentpolicy.DecisionClarify, Error: "", Inputs: nil}}, intentpolicy.DecisionClarify, []string{"b"}},
		{"explicit permit", []intentpolicy.ContractOutcome{{ContractID: "a", Result: intentpolicy.DecisionPermit, Error: "", Inputs: nil}, {ContractID: "b", Result: "abstain", Error: "", Inputs: nil}}, intentpolicy.DecisionPermit, []string{"a"}},
		{"all abstain defaults to permit", []intentpolicy.ContractOutcome{{ContractID: "a", Result: "abstain", Error: "", Inputs: nil}}, intentpolicy.DecisionPermit, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, by := intentpolicy.Combine(tc.outcomes)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.by, by)
		})
	}
}
