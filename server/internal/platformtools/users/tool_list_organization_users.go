package users

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListOrganizationUsers struct {
	organizations OrganizationsService
}

type listOrganizationUsersInput struct{}

func NewListOrganizationUsersTool(orgSvc OrganizationsService) *ListOrganizationUsers {
	return &ListOrganizationUsers{organizations: orgSvc}
}

func (s *ListOrganizationUsers) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "users",
		HandlerName: "list_organization_users",
		Name:        "platform_list_organization_users",
		Description: "List the Gram users linked to the current organization (the internal directory the assistant resolves names against).",
		InputSchema: core.BuildInputSchema[listOrganizationUsersInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListOrganizationUsers) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.organizations == nil {
		return fmt.Errorf("organizations service not configured")
	}

	input := listOrganizationUsersInput{}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}

	result, err := s.organizations.ListUsers(ctx, &organizations.ListUsersPayload{
		SessionToken: nil,
	})
	if err != nil {
		return fmt.Errorf("list organization users: %w", err)
	}

	return encodeToolResult(wr, result)
}
