package researchagent

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
)

// ScannerJudge adapts the prompt-injection scanner the risk pipeline already
// runs to the verdict this package needs. One judge, one prompt, one place
// where "is this content trying to steer its reader" is decided — a research
// run must not develop its own second opinion about what an attack is.
type ScannerJudge struct {
	scanner *promptinjection.Scanner
}

// NewScannerJudge wraps a prompt-injection scanner as an InjectionJudge.
func NewScannerJudge(scanner *promptinjection.Scanner) *ScannerJudge {
	return &ScannerJudge{scanner: scanner}
}

var _ InjectionJudge = (*ScannerJudge)(nil)

// JudgeFetchedPage classifies one fetched page.
//
// It scans strictly: a judge that reached no verdict is an error here, not a
// clean page. The gating paths fail open because a judge outage must never
// block a developer's tool call; this path records evidence instead, and
// evidence that nothing was found has to mean something was looked at.
func (j *ScannerJudge) JudgeFetchedPage(ctx context.Context, input JudgeInput) (JudgeVerdict, error) {
	findings, err := j.scanner.ScanStrict(ctx, input.Content, input.OrgID, input.ProjectID, "", judgemessage.Message{
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

	if len(findings) == 0 {
		return JudgeVerdict{Injection: false, Rationale: ""}, nil
	}

	return JudgeVerdict{Injection: true, Rationale: findings[0].Description}, nil
}
