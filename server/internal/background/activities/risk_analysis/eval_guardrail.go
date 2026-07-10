package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
)

// EvalMessage is a recorded chat message fed to the guardrail-eval replay. It is
// deliberately row-agnostic (built from primitive columns) so callers outside
// this package can drive the replay without importing the risk repo row type.
type EvalMessage struct {
	ID        uuid.UUID
	Role      string
	Content   string
	ToolCalls []byte
}

// MessageVerdict is the judge's verdict for one in-scope EvalMessage. Index
// points back into the input slice so callers can attach message metadata.
type MessageVerdict struct {
	Index     int
	Type      message.Type
	ToolName  string
	LatencyMs int64
	promptpolicy.Verdict
}

// EvalPromptGuardrail replays a prompt_based guardrail against a sequence of
// chat messages and returns one verdict per in-scope message, ordered by input
// index. It reuses the exact role-to-type mapping, tool-call flattening, and judge
// prompt the batch analyzer runs, so a workbench replay matches production
// judging. It performs no writes, enforcement, or outbox side effects.
//
// Scoping is by message_types and CEL scope predicates; when both are empty
// every supported message is judged.
func EvalPromptGuardrail(
	ctx context.Context,
	logger *slog.Logger,
	judge promptpolicy.Evaluator,
	eng *celenv.Engine,
	orgID, projectID, prompt string,
	cfg promptpolicy.Config,
	messages []EvalMessage,
	messageTypes []string,
	includeCEL, exemptCEL string,
) ([]MessageVerdict, error) {
	scope, err := CompileScope(eng, includeCEL, exemptCEL)
	if err != nil {
		return nil, fmt.Errorf("compile eval scope: %w", err)
	}

	inScope := make([]int, 0, len(messages))
	built := make([]batchMessage, len(messages))
	for i, m := range messages {
		msg, ok := newBatchMessage(ctx, logger, m.ID, m.Role, m.Content, m.ToolCalls)
		if !ok {
			continue
		}
		if len(messageTypes) > 0 && !slices.Contains(messageTypes, msg.Type) {
			continue
		}
		ok, err := scope.InScope(batchMessageView(msg))
		if err != nil {
			return nil, fmt.Errorf("eval scope for message %s: %w", m.ID, err)
		}
		if !ok {
			continue
		}
		built[i] = msg
		inScope = append(inScope, i)
	}

	verdicts := make([]MessageVerdict, len(inScope))
	if len(inScope) == 0 {
		return verdicts, nil
	}

	if judge == nil || strings.TrimSpace(prompt) == "" {
		for vi, idx := range inScope {
			verdicts[vi] = messageVerdictSkeleton(idx, built[idx])
			if !cfg.FailOpen {
				applyFailClosedFallback(&verdicts[vi], nil)
			}
		}
		return verdicts, nil
	}

	judgeFanout(
		ctx, judge, orgID, projectID, prompt, cfg, built, inScope,
		func(pos, idx int, verdict *promptpolicy.Verdict, err error, latency time.Duration) {
			out := messageVerdictSkeleton(idx, built[idx])
			out.LatencyMs = latency.Milliseconds()
			if err != nil && !cfg.FailOpen {
				applyFailClosedFallback(&out, err)
			} else if verdict != nil {
				out.Verdict = *verdict
			}
			verdicts[pos] = out
		},
		nil,
	)
	return verdicts, nil
}

// messageVerdictSkeleton builds the un-matched verdict carrying the message's
// display metadata (type, single-call tool name).
func messageVerdictSkeleton(idx int, msg batchMessage) MessageVerdict {
	toolName := ""
	if msg.Type == message.ToolRequest && len(msg.ToolCalls) == 1 {
		toolName = msg.ToolCalls[0].Function.Name
	}
	return MessageVerdict{
		Index:     idx,
		Type:      msg.Type,
		ToolName:  toolName,
		LatencyMs: 0,
		Verdict: promptpolicy.Verdict{
			Matched:          false,
			Confidence:       0,
			Rationale:        "",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}
}

func applyFailClosedFallback(verdict *MessageVerdict, err error) {
	verdict.Verdict = promptpolicy.FailClosedVerdict(err)
}
