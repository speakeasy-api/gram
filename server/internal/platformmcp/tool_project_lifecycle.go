//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	createProjectToolName = "create_project"
	renameProjectToolName = "rename_project"

	unavailableProjectLifecycleMessage = "Creating or renaming projects is not available on this server."
)

// registerProjectLifecycleTools keeps the live and unavailable manifests
// identical, so the tools never appear on and disappear from the catalogue as
// a deployment composes or fails to compose the service behind them.
//
// Both stay off the managed assistant. An assistant is bound to the one
// project it serves, so creating another project is outside its reach by
// construction, and renaming the project it lives in is an organization
// administrator's decision rather than something a project assistant should
// be able to do on its own.
func registerProjectLifecycleTools(reg *Registrar, service *ProjectLifecycleService) {
	create := unavailableProjectLifecycleHandler[CreateProjectInput]()
	rename := unavailableProjectLifecycleHandler[RenameProjectInput]()
	if service.valid() {
		create = func(ctx context.Context, _ *mcp.CallToolRequest, input CreateProjectInput) (*mcp.CallToolResult, ProjectMutationOutput, error) {
			return principalToolCall(ctx, projectLifecycleToolResult, func(principal Principal) (ProjectMutationOutput, error) {
				return service.CreateProject(ctx, principal, input)
			})
		}
		rename = func(ctx context.Context, _ *mcp.CallToolRequest, input RenameProjectInput) (*mcp.CallToolResult, ProjectMutationOutput, error) {
			return principalToolCall(ctx, projectLifecycleToolResult, func(principal Principal) (ProjectMutationOutput, error) {
				return service.RenameProject(ctx, principal, input)
			})
		}
	}

	addTool(reg, &mcp.Tool{
		Name:  createProjectToolName,
		Title: "Create a Project",
		Description: "Create an empty project in this organization. A project is where MCP servers and skills are kept before anyone receives them; creating one grants nothing and reaches nobody until something is added to it and shared through a plugin. " +
			"Supply only a display name: the project's slug, which addresses it in dashboard links and never changes, comes from the name exactly as the dashboard derives it and cannot be chosen. Do not work the slug out yourself. Call first without confirmed: true: nothing is created, and the confirmation_required refusal returns the exact name and slug this call would create. Show that slug to the user. " +
			"Then call again with confirmed: true, only after the user confirms the exact name and slug. Choose one idempotency key for this create and pass it on both the preview and the confirmed call; the preview records nothing under it. Use a fresh key only for a new attempt after a refusal. A retry with the same key and name returns the project the first call created instead of making a second one. " +
			"A name with no letters or digits is refused, and a name whose slug matches an existing project is refused as a conflict; nothing is created in either case. The new project comes with the Default environment and Default plugin every project gets.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly,
		// Organization-scoped: there is no project until this call makes one.
		ProjectScope: ProjectScopeNone,
	}, create)
	addTool(reg, &mcp.Tool{
		Name:  renameProjectToolName,
		Title: "Rename a Project",
		Description: "Change the display name of one exact project. Nothing else changes: the project's slug stays the same, so dashboard links and anything addressing the project by slug keep working, and its MCP servers, skills, plugins, and their audiences are untouched. " +
			"Supply the project ID from list_projects, an idempotency key, and confirmed: true only after the user confirms the exact project and new name. " +
			"Organization administrator access is not enough on its own: the caller also needs write access to that exact project, as in the dashboard, and is refused with the missing permission otherwise.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly,
		ProjectScope: ProjectScopeExplicit,
	}, rename)
}

func unavailableProjectLifecycleHandler[In any]() mcp.ToolHandlerFor[In, ProjectMutationOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, ProjectMutationOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: projectLifecycleFeature, Message: unavailableProjectLifecycleMessage})
		if err != nil {
			return nil, ProjectMutationOutput{}, fmt.Errorf("encode unavailable project lifecycle result: %w", err)
		}
		return nil, ProjectMutationOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

type projectLifecycleRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`

	// Name and Slug are present only on an unconfirmed create: the exact
	// project the confirmed call would make, including the slug the user must
	// see before confirming because it is derived and never changes.
	Name string `json:"name,omitempty"`
	Slug string `json:"slug,omitempty"`
}

func projectLifecycleToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	var refusal projectLifecycleRefusal
	if preview, ok := errors.AsType[*ProjectCreatePreviewError](err); ok {
		refusal = projectLifecycleRefusal{Code: "confirmation_required", Feature: projectLifecycleFeature, Message: preview.Error(), Name: preview.Name, Slug: preview.Slug}
	} else if lifecycle, ok := errors.AsType[*ProjectLifecycleError](err); ok {
		refusal = projectLifecycleRefusal{Code: lifecycle.Code, Feature: projectLifecycleFeature, Message: lifecycle.Message, Name: "", Slug: ""}
	} else {
		return nil, false
	}
	payload, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
