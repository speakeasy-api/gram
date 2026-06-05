// Package insights provides the managed assistant's observability tools that are
// backed by management services other than telemetry (deployments, chat,
// organizations, risk) — restoring the rest of the catalog the old client-side
// AI Insights copilot had. Telemetry-backed tools live in the sibling logs
// package.
//
// Each tool wraps a management-service method and passes nil auth tokens: the
// assistant runtime supplies the project/org/user context (and, for dashboard
// turns, the sending user's grants), so every wrapped method's own authz check
// self-enforces per user. Services are injected as providers (func() Service)
// rather than values because the managed-assistant toolset is built early at
// startup — before chat (and friends) are constructed — to avoid a
// toolset → mcpService → chatService → toolset construction cycle.
package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
)

type DeploymentsService interface {
	GetDeploymentLogs(ctx context.Context, payload *deployments.GetDeploymentLogsPayload) (*deployments.GetDeploymentLogsResult, error)
}

type ChatService interface {
	ListChats(ctx context.Context, payload *chat.ListChatsPayload) (*chat.ListChatsResult, error)
	LoadChat(ctx context.Context, payload *chat.LoadChatPayload) (*chat.Chat, error)
}

type OrganizationsService interface {
	ListUsers(ctx context.Context, payload *organizations.ListUsersPayload) (*organizations.ListUsersResult, error)
}

type RiskService interface {
	ListRiskPolicies(ctx context.Context, payload *risk.ListRiskPoliciesPayload) (*risk.ListRiskPoliciesResult, error)
	ListRiskResultsForAgent(ctx context.Context, payload *risk.ListRiskResultsForAgentPayload) (*risk.ListRiskResultsForAgentResult, error)
	ListRiskResultsByChat(ctx context.Context, payload *risk.ListRiskResultsByChatPayload) (*risk.ListRiskResultsByChatResult, error)
	GetRiskPolicyStatus(ctx context.Context, payload *risk.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error)
}

// readOnlyToolAnnotations is the annotation set shared by every tool here: safe
// to call, non-destructive, idempotent, and closed-world.
func readOnlyToolAnnotations() *types.ToolAnnotations {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

// decodeToolInput reads a tool's JSON request body into dst, tolerating an empty
// body so callers can rely on the defaults they pre-populate dst with.
func decodeToolInput(payload io.Reader, dst any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

// encodeToolResult writes a result as JSON. Results flow through an `any`, so
// musttag can't (and doesn't need to) inspect the Goa-generated result types.
func encodeToolResult(wr io.Writer, result any) error {
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
