package researchagent

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
)

// ScannerJudge adapts the prompt-injection scanner the risk pipeline already
// runs to the verdict this package needs. One judge, one prompt, one place
// where "is this content trying to steer its reader" is decided — a research
// run must not develop its own second opinion about what an attack is.
type ScannerJudge struct {
	scanner      *promptinjection.Scanner
	riskRecorder *metering.RiskRecorder
}

// NewScannerJudge wraps a prompt-injection scanner as an InjectionJudge.
func NewScannerJudge(scanner *promptinjection.Scanner, riskRecorder *metering.RiskRecorder) *ScannerJudge {
	return &ScannerJudge{scanner: scanner, riskRecorder: riskRecorder}
}

var _ InjectionJudge = (*ScannerJudge)(nil)

// JudgeFetchedPage classifies one fetched page.
//
// It scans strictly: a judge that reached no verdict is an error here, not a
// clean page. The gating paths fail open because a judge outage must never
// block a developer's tool call; this path records evidence instead, and
// evidence that nothing was found has to mean something was looked at.
func (j *ScannerJudge) JudgeFetchedPage(ctx context.Context, input JudgeInput) (JudgeVerdict, error) {
	startedAt := time.Now().UTC()
	result, providerResult, err := j.scanner.ScanStrictWithVerdict(ctx, input.Content, input.OrgID, input.ProjectID, "", judgemessage.Message{
		// The page is tool output as far as the judge is concerned: content
		// that arrived from outside and is being read by an agent.
		Type:        message.ToolResponse,
		Body:        input.Content,
		ToolName:    "platform_fetch_page",
		MCPServer:   "",
		MCPFunction: "",
		ToolCalls:   nil,
	})
	if err != nil {
		// ErrNoVerdict included: an unavailable judge is a page nobody
		// looked at, which the caller must count as such.
		return JudgeVerdict{Injection: false, Rationale: ""}, fmt.Errorf("judge fetched page %s: %w", input.URL, err)
	}

	if result.Completed {
		projectID, parseErr := uuid.Parse(input.ProjectID)
		if parseErr != nil {
			return JudgeVerdict{Injection: false, Rationale: ""}, fmt.Errorf("parse fetched page project id: %w", parseErr)
		}
		provenance := metering.RiskProvenance{
			OrganizationID:    input.OrgID,
			ProjectID:         projectID,
			RiskPolicyID:      uuid.Nil,
			RiskPolicyVersion: 0,
			PolicyLinkReason:  "research_agent_fetch_scan",
			ChatID:            uuid.Nil,
			ChatMessageID:     uuid.Nil,
			ContentPartID:     uuid.Nil,
			MessageLinkReason: "research_fetched_page_unlinked",
			OperationID:       fmt.Sprintf("research_agent:%s:tool_call:%s", input.ReportID, input.ToolCallID),
			ExecutionPath:     "research_agent",
			RequestID:         input.ReportID.String(),
			MessageType:       message.ToolResponse,
			HookSource:        "",
			UserID:            "",
			ToolCallID:        input.ToolCallID,
			ToolName:          input.ToolName,
			Model:             providerResult.Model,
			Provider:          providerResult.Provider,
		}
		_ = j.riskRecorder.Record(ctx, metering.RiskPromptInjection(), provenance, result.STokens, startedAt)
	}

	if len(result.Findings) == 0 {
		return JudgeVerdict{Injection: false, Rationale: ""}, nil
	}

	return JudgeVerdict{Injection: true, Rationale: result.Findings[0].Description}, nil
}
