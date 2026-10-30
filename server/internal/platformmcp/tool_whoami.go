//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	whoamiToolName        = "whoami"
	whoamiToolTitle       = "Show Who I Am Connected As"
	whoamiToolDescription = "Show who this session is connected as: the user (email and display name), the active organization (ID, name, slug), the user's live roles in it, and the name the connecting client registered. On an external connection it also lists the user's other organizations; to work in one of them, the user must reconnect and pick it. Use this to answer identity questions instead of guessing from other tools. Present names and email to the user, not IDs."
)

func registerWhoamiTool(reg *Registrar, whoami *WhoamiService) {
	tool := &mcp.Tool{
		Name:        whoamiToolName,
		Title:       whoamiToolTitle,
		Description: whoamiToolDescription,
		Annotations: readOnlyAnnotations(),
		InputSchema: whoamiInputSchema(),
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}
	if whoami == nil {
		addTool(reg, tool, meta, unavailableTool(whoamiToolName))
		return
	}
	tool.Meta = cardToolMeta()
	addTool(reg, tool, meta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, WhoamiOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, WhoamiOutput{}, err
		}
		output, err := whoami.Whoami(ctx, principal)
		if err != nil {
			return nil, WhoamiOutput{}, err
		}
		return cardResult(whoamiCard(output)), output, nil
	})
}

func whoamiInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[struct{}](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer whoami input schema: %v", err))
	}
	return schema
}
