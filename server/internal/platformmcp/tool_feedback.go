//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SendPlatformMCPFeedbackToolInput struct {
	Category        string `json:"category" jsonschema:"feedback category: success, failure, confusing_guidance, missing_capability, incorrect_result, authorization_problem, setup_problem, performance, or other"`
	Rating          *int   `json:"rating,omitempty" jsonschema:"optional rating from 1 through 5"`
	Success         *bool  `json:"success,omitempty" jsonschema:"optional success outcome"`
	ToolName        string `json:"tool_name,omitempty" jsonschema:"optional known Platform MCP tool name; do not include remote tool names"`
	FailureCategory string `json:"failure_category,omitempty" jsonschema:"optional allowlisted failure or readiness category"`
	Note            string `json:"note,omitempty" jsonschema:"optional feedback note, at most 500 characters; never include credentials, URLs, identifiers, requests, responses, logs, or headers"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"caller-generated retry key, at most 128 characters; reuse only for the same feedback"`
}

type SendPlatformMCPFeedbackToolOutput struct {
	TrackingID    string `json:"tracking_id"`
	DeliveryState string `json:"delivery_state"`
	ExpiresAt     string `json:"expires_at"`
	Replayed      bool   `json:"replayed"`
}

func feedbackToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrFeedbackRateLimited):
		result = operationBudgetResult{Code: "rate_limited", Message: "Feedback was sent too often just now. Try again shortly."}
	case errors.Is(err, ErrFeedbackInvalid):
		result = operationBudgetResult{Code: "invalid_input", Message: "Feedback must use the fields described here, and must not contain anything sensitive or identifying."}
	case errors.Is(err, ErrFeedbackConflict):
		result = operationBudgetResult{Code: "conflict", Message: "That retry key was already used for different feedback. Use a new one."}
	case errors.Is(err, ErrFeedbackForbidden):
		result = operationBudgetResult{Code: "forbidden", Message: "This session is no longer connected. Reconnect and try again."}
	case errors.Is(err, ErrFeedbackUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Message: "Feedback is not switched on for your organization yet."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func registerFeedbackTool(reg *Registrar, feedback *FeedbackService) {
	addTool(reg, &mcp.Tool{
		Name:        "send_platform_mcp_feedback",
		Title:       "Send Feedback About This Platform",
		Description: "Store one short feedback report about this platform for the team to read. Ask the user for consent before submitting. Constraints: never include credentials, URLs, identifiers, payloads, logs, headers, or attachments.",
	}, ToolMeta{
		Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input SendPlatformMCPFeedbackToolInput) (*mcp.CallToolResult, SendPlatformMCPFeedbackToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, SendPlatformMCPFeedbackToolOutput{}, err
		}
		result, err := feedback.Submit(ctx, principal, FeedbackInput(input))
		if err != nil {
			if feedbackResult, ok := feedbackToolResult(err); ok {
				return feedbackResult, SendPlatformMCPFeedbackToolOutput{}, nil
			}
			return nil, SendPlatformMCPFeedbackToolOutput{}, err
		}
		return nil, SendPlatformMCPFeedbackToolOutput{
			TrackingID:    result.TrackingID,
			DeliveryState: result.DeliveryState,
			ExpiresAt:     result.ExpiresAt.UTC().Format(time.RFC3339),
			Replayed:      result.Replayed,
		}, nil
	})
}
