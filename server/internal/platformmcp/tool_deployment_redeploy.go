//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func registerDeploymentRedeployTool(reg *Registrar, service *DeploymentRedeployService) {
	// Match the dashboard Redeploy endpoint: project:write on the exact
	// project, not org:admin. Live external membership is checked by runtime;
	// the service resolves the project within that organization and reuses
	// the canonical deployment service's authorization.
	addTool(reg, &mcp.Tool{
		Name:  redeployDeploymentToolName,
		Title: "Redeploy a Project Deployment",
		Description: "Clone and reprocess an exact project deployment snapshot, including all of its OpenAPI, function, package and external MCP sources. This is project-wide, not a restart of one MCP server or upstream provider. An older snapshot can roll back newer tools and configuration. Requires project:write and project:read; external user clients only. " +
			"First call with confirmed:false, an exact project_id and a stable idempotency_key; omit deployment_id only to preview the active snapshot. Show the returned source_deployment_id, active_deployment_id, source counts, and whole-project impact to the user. After explicit confirmation, resend the same key with deployment_id set to that source ID, expected_active_deployment_id set to the preview's active ID (or none), and confirmed:true. A changed active deployment is refused; preview and reconfirm before making a new attempt. " +
			"Processing is dispatched without waiting for completion, and activation checks the expected active snapshot again atomically; if it changed while processing, the clone fails instead of replacing it. The same key and input reuse the committed clone instead of creating another deployment. Keep them unchanged on timeout or unavailable processing; do not automatically use a new key. Processing can fail or still be pending: report the returned status, verified and active fields, never claim that tools are ready just because a clone exists. A verification_unavailable result still identifies a committed clone; inspect its dashboard_path before proceeding. No source contents, secrets or deployment logs are returned.",
		Annotations: writeAnnotations(true, true, true),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input RedeployDeploymentInput) (*mcp.CallToolResult, RedeployDeploymentOutput, error) {
		return principalToolCall(ctx, deploymentRedeployToolResult, func(principal Principal) (RedeployDeploymentOutput, error) {
			if !service.valid() {
				return RedeployDeploymentOutput{}, ErrUnavailable
			}
			return service.Redeploy(ctx, principal, input)
		})
	})
}

func deploymentRedeployToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	if result, ok := operationBudgetToolResult(err); ok {
		return result, true
	}
	result := struct {
		Code    string                     `json:"code"`
		Message string                     `json:"message"`
		Preview *DeploymentRedeployPreview `json:"preview,omitempty"`
	}{Code: unavailableCode, Message: "Deployment redeployment is temporarily unavailable. Keep the same idempotency key when retrying; never assume a timeout means no deployment was created.", Preview: nil}
	var refusal *deploymentRedeployError
	var shared *oops.ShareableError
	switch {
	case errors.As(err, &refusal):
		result.Code = refusal.Code
		result.Message = refusal.Message
		result.Preview = refusal.Preview
	case errors.Is(err, ErrForbidden):
		result.Code = "not_found"
		result.Message = "That deployment or project is not available to you."
	case errors.As(err, &shared):
		switch shared.Code {
		case oops.CodeInvalid, oops.CodeBadRequest:
			result.Code = "invalid_request"
			result.Message = "Provide exact project and deployment IDs and explicitly confirm the preview."
		case oops.CodeNotFound:
			result.Code = "not_found"
			result.Message = "That deployment or project is not available to you."
		case oops.CodeUnauthorized, oops.CodeForbidden:
			result.Code = "permission_denied"
			result.Message = "Redeployment requires project:write, project:read and a user identity."
		case oops.CodeConflict:
			result.Code = "conflict"
			result.Message = "The deployment changed. Preview again before confirming a new attempt."
		default:
			// Keep the bounded unavailable response for other backend errors.
		}
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
