//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const recordWorkflowRunToolName = "record_workflow_run"

const recordWorkflowRunToolDescription = "Record what one run of a Speakeasy workflow handled, so the Speakeasy team can debug that workflow while it is new. Call this only when the user explicitly asked for a diagnostics run; it is off by default. Tell the user what will be sent before sending it. Include every item the run handled, including the ones it excluded, with the reason. Constraints: send the sanitized endpoint only, never credentials, headers, tokens, environment values, or local commands."

type RecordWorkflowRunToolInput struct {
	Skill       string            `json:"skill" jsonschema:"name of the workflow that ran, for example add-existing-mcp-servers"`
	RunID       string            `json:"run_id" jsonschema:"caller-generated identifier grouping this run's events, at most 128 characters"`
	ProjectSlug string            `json:"project_slug,omitempty" jsonschema:"project slug the run targeted, when the workflow targets one"`
	Client      string            `json:"client,omitempty" jsonschema:"local client the run was driven from, for example claude_code"`
	Items       []WorkflowRunItem `json:"items" jsonschema:"everything the run handled, including the items it excluded; send an empty list when the run handled nothing"`
}

type RecordWorkflowRunToolOutput struct {
	Recorded bool `json:"recorded"`
}

func workflowRunToolResult(err error) (*mcp.CallToolResult, bool) {
	if !errors.Is(err, ErrWorkflowRunInvalid) {
		return nil, false
	}
	content, marshalErr := json.Marshal(operationBudgetResult{
		Code:    "invalid_input",
		Message: "The diagnostics report must use the fields described here.",
	})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func registerWorkflowRunTool(reg *Registrar, workflowRun *WorkflowRunService) {
	if !workflowRun.valid() {
		addTool(reg, &mcp.Tool{
			Name:        recordWorkflowRunToolName,
			Title:       "Record Workflow Run",
			Description: "Record what one run of a Speakeasy workflow handled. This is not switched on for your organization yet.",
		}, ToolMeta{
			Authorization: ExternalAuthorizationOrgAdmin,
			Audiences:     bothAudiences,
			ProjectScope:  ProjectScopeNone,
		}, unavailableTool("platform_mcp_workflow_run_reporting"))
		return
	}

	addTool(reg, &mcp.Tool{
		Name:        recordWorkflowRunToolName,
		Title:       "Record Workflow Run",
		Description: recordWorkflowRunToolDescription,
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		Audiences:     bothAudiences,
		ProjectScope:  ProjectScopeNone,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RecordWorkflowRunToolInput) (*mcp.CallToolResult, RecordWorkflowRunToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, RecordWorkflowRunToolOutput{Recorded: false}, err
		}
		if err := workflowRun.Record(ctx, principal, WorkflowRunInput(input)); err != nil {
			if toolResult, ok := workflowRunToolResult(err); ok {
				return toolResult, RecordWorkflowRunToolOutput{Recorded: false}, nil
			}
			// A delivery failure is logged rather than raised: diagnostics must
			// never turn a completed workflow into a failed one.
			workflowRun.logEmitFailure(ctx, principal.OrganizationID, err)
			return nil, RecordWorkflowRunToolOutput{Recorded: false}, nil
		}
		return nil, RecordWorkflowRunToolOutput{Recorded: true}, nil
	})
}
