package promptpolicy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureEnforcementReply struct {
	reply *riskv1.EnforcementReply
}

func (w *captureEnforcementReply) Reply(_ context.Context, _ string, reply *riskv1.EnforcementReply) error {
	w.reply = reply
	return nil
}

type incompletePromptPolicyScanner struct {
	config Config
}

func (s *incompletePromptPolicyScanner) ScanWithVerdict(_ context.Context, _, _, _, _ string, config Config, _ judgemessage.Message) (scanners.Result, *Verdict) {
	s.config = config
	return scanners.Result{
		Findings:  []scanners.Finding{NewFinding(Verdict{Matched: true, Rationale: "judge unavailable"})},
		Completed: false,
	}, nil
}

func TestEnforceHandlerPreservesFailClosedFindingOnIncompleteJudge(t *testing.T) {
	t.Parallel()

	scanner := &incompletePromptPolicyScanner{}
	writer := &captureEnforcementReply{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		scanner,
		writer,
		nil,
		EnforceHandlerConfig{},
	)
	require.NoError(t, err)

	policyID := "0198f1f4-0000-7000-8000-000000000001"
	correlationID := "0198f1f4-0000-7000-8000-000000000000"
	err = handler.Handle(t.Context(), riskv1.PromptPolicyEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		RiskPolicyId:   new(policyID),
		Content:        new("content"),
		Body:           new("judge this"),
		Prompt:         new("deny unsafe requests"),
		ModelConfig:    []byte(`{"fail_open":false}`),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:" + correlationID,
	}})
	require.NoError(t, err)
	require.False(t, scanner.config.FailOpen)
	require.NotNil(t, writer.reply)
	require.Equal(t, policyID, writer.reply.GetPolicyId())
	require.True(t, writer.reply.GetIncomplete())
	require.Len(t, writer.reply.GetFindings(), 1)
	require.Equal(t, Rule, writer.reply.GetFindings()[0].GetRuleId())
}
