package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListOrganizationUsers struct {
	provider func() OrganizationsService
}

type listOrganizationUsersInput struct{}

func NewListOrganizationUsersTool(provider func() OrganizationsService) *ListOrganizationUsers {
	return &ListOrganizationUsers{provider: provider}
}

func (s *ListOrganizationUsers) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "list_organization_users",
		Name:        "platform_list_organization_users",
		Description: "List the members (users) of the current organization. Requires organization read access.",
		InputSchema: core.BuildInputSchema[listOrganizationUsersInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListOrganizationUsers) Call(ctx context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("organizations service not configured")
	}

	result, err := svc.ListUsers(ctx, &organizations.ListUsersPayload{SessionToken: nil})
	if err != nil {
		return fmt.Errorf("list organization users: %w", err)
	}

	return encodeToolResult(wr, result)
}
