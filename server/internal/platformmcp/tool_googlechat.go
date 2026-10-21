package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	gen "github.com/speakeasy-api/gram/server/gen/google_chat_connections"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections"
)

type googleChatListInput struct{}
type googleChatBindingInput struct {
	BindingID string `json:"binding_id"`
	Revision  int64  `json:"revision"`
	Action    string `json:"action"`
	Confirmed bool   `json:"confirmed"`
}
type googleChatMappingInput struct {
	BindingID string  `json:"binding_id"`
	MemberID  string  `json:"member_id"`
	UserID    *string `json:"user_id,omitempty"`
	Revision  int64   `json:"revision"`
	Confirmed bool    `json:"confirmed"`
}
type googleChatConfigureInput struct {
	ConnectionID string   `json:"connection_id" jsonschema:"Exact connected Google account ID from list_google_chat_connections"`
	ProjectID    string   `json:"project_id" jsonschema:"Explicit project UUID that owns the assistant"`
	AssistantID  string   `json:"assistant_id" jsonschema:"Exact assistant UUID to add to the space"`
	SpaceName    *string  `json:"space_name,omitempty" jsonschema:"Existing accessible space resource, or omit to create a new external space"`
	DisplayName  string   `json:"display_name" jsonschema:"Name for the new space"`
	Prefix       string   `json:"prefix" jsonschema:"Prefix to start a conversation, for example assistant:"`
	InviteEmails []string `json:"invite_emails" jsonschema:"Participants to invite; only mapped Speakeasy identities can invoke the assistant"`
	RequestID    string   `json:"request_id" jsonschema:"UUID idempotency key; reuse unchanged only for retries of this exact setup"`
	Confirmed    bool     `json:"confirmed" jsonschema:"True only after approval of the assistant, shared audience, and attached Chat tools"`
}

type googleChatToolOutput struct {
	Status      string                 `json:"status"`
	Connections *gen.ListResult        `json:"connections,omitempty"`
	Members     *gen.ListMembersResult `json:"members,omitempty"`
	Binding     *gen.GoogleChatBinding `json:"binding,omitempty"`
	Member      *gen.GoogleChatMember  `json:"member,omitempty"`
}

func registerGoogleChatTools(reg *Registrar, service *googlechatconnections.Service) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: nil}
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "Connect an assistant to Google Chat", Icons: nil, Name: "configure_google_chat_space", Description: "After an administrator connects Google in Org > Identity, create or select a shared Chat space for an exact assistant and invite participants. Automatically attaches Chat tools and event delivery. Confirm the shared audience first. Requires app approval in Google Workspace. Reuse request_id only for the same setup. Inspect the returned status; error or pending requires repair.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false), ReadOnlyHint: false, IdempotentHint: true, OpenWorldHint: new(true), Title: ""}}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input googleChatConfigureInput) (*mcp.CallToolResult, googleChatToolOutput, error) {
		if service == nil {
			return nil, googleChatToolOutput{}, ErrUnavailable
		}
		if !input.Confirmed {
			return nil, googleChatToolOutput{Status: "confirmation_required", Connections: nil, Members: nil, Binding: nil, Member: nil}, nil
		}
		binding, err := service.Bind(ctx, &gen.BindPayload{SessionToken: nil, ConnectionID: input.ConnectionID, ProjectID: input.ProjectID, AssistantID: input.AssistantID, SpaceName: input.SpaceName, DisplayName: input.DisplayName, Prefix: input.Prefix, InviteEmails: input.InviteEmails, RequestID: input.RequestID})
		if err != nil {
			return nil, googleChatToolOutput{}, fmt.Errorf("configure Google Chat space: %w", err)
		}
		return nil, googleChatToolOutput{Status: binding.Status, Binding: binding, Connections: nil, Members: nil, Member: nil}, nil
	})

	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "", Icons: nil, Name: "list_google_chat_connections", Description: "List connected Google accounts and assistant space bindings in this organization. No credentials are returned. Google consent must be completed in Org > Identity > Google Chat.", Annotations: readOnlyAnnotations()}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, _ googleChatListInput) (*mcp.CallToolResult, googleChatToolOutput, error) {
		if service == nil {
			return nil, googleChatToolOutput{}, ErrUnavailable
		}
		result, err := service.List(ctx, &gen.ListPayload{SessionToken: nil})
		if err != nil {
			return nil, googleChatToolOutput{}, fmt.Errorf("list Google Chat: %w", err)
		}
		return nil, googleChatToolOutput{Status: "ok", Connections: result, Members: nil, Binding: nil, Member: nil}, nil
	})
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "", Icons: nil, Name: "manage_google_chat_space", Description: "Inspect participants or pause, resume, repair, or delete an exact Google Chat binding. Pass action inspect to read members. Mutations require its latest revision and confirmed=true after the user approves the exact space and action. Unconfirmed mutations make no changes.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: new(true), Title: ""}}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input googleChatBindingInput) (*mcp.CallToolResult, googleChatToolOutput, error) {
		if service == nil {
			return nil, googleChatToolOutput{}, ErrUnavailable
		}
		if input.Action == "inspect" {
			members, err := service.ListMembers(ctx, &gen.ListMembersPayload{SessionToken: nil, BindingID: input.BindingID})
			if err != nil {
				return nil, googleChatToolOutput{}, fmt.Errorf("inspect Google Chat: %w", err)
			}
			return nil, googleChatToolOutput{Status: "ok", Members: members, Connections: nil, Binding: nil, Member: nil}, nil
		}
		if !input.Confirmed {
			return nil, googleChatToolOutput{Status: "confirmation_required", Connections: nil, Members: nil, Binding: nil, Member: nil}, nil
		}
		binding, err := service.UpdateBinding(ctx, &gen.UpdateBindingPayload{SessionToken: nil, BindingID: input.BindingID, Revision: input.Revision, Action: input.Action})
		if err != nil {
			return nil, googleChatToolOutput{}, fmt.Errorf("manage Google Chat: %w", err)
		}
		return nil, googleChatToolOutput{Status: "ok", Binding: binding, Connections: nil, Members: nil, Member: nil}, nil
	})
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Title: "", Icons: nil, Name: "map_google_chat_identity", Description: "Associate one observed Google participant with an existing active Speakeasy person, or omit user_id to revoke a mapping. Requires the latest mapping revision. Grants no new permissions. Pass confirmed=true only after the user approves the exact participant and person; otherwise no change occurs.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: new(true), Title: ""}}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input googleChatMappingInput) (*mcp.CallToolResult, googleChatToolOutput, error) {
		if service == nil {
			return nil, googleChatToolOutput{}, ErrUnavailable
		}
		if !input.Confirmed {
			return nil, googleChatToolOutput{Status: "confirmation_required", Connections: nil, Members: nil, Binding: nil, Member: nil}, nil
		}
		member, err := service.MapMember(ctx, &gen.MapMemberPayload{SessionToken: nil, BindingID: input.BindingID, MemberID: input.MemberID, UserID: input.UserID, Revision: input.Revision})
		if err != nil {
			return nil, googleChatToolOutput{}, fmt.Errorf("map Google Chat identity: %w", err)
		}
		return nil, googleChatToolOutput{Status: "ok", Member: member, Connections: nil, Members: nil, Binding: nil}, nil
	})
}
