package usersessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) reviewGatewayInventory(ctx context.Context, id string) (*toolfilter.FrozenToolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagGatewayFrozenToolsets, authCtx.ActiveOrganizationID, feature.OrgProjectGroups(authCtx.OrganizationSlug, ""))
	if err != nil || !enabled || s.gatewayInventory == nil {
		return nil, oops.E(oops.CodeForbidden, err, "frozen toolsets are not available")
	}
	target, err := s.resolveMetaServerMintTarget(ctx, id, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, target.resourceID, authCtx.ProjectID.String())); err != nil {
		return nil, oops.E(oops.CodeForbidden, err, "gateway connection permission required")
	}
	gatewayID, err := uuid.Parse(target.resourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid gateway")
	}
	inventory, err := s.gatewayInventory.GatewayInventory(ctx, gatewayID, *authCtx.ProjectID, urn.NewUserSubject(authCtx.UserID))
	if err != nil {
		return nil, fmt.Errorf("read complete gateway inventory: %w", err)
	}
	return inventory, nil
}

func (s *Service) PreviewGatewayToolset(ctx context.Context, payload *gen.PreviewGatewayToolsetPayload) (*gen.GatewayToolsetReview, error) {
	inventory, err := s.reviewGatewayInventory(ctx, payload.MetaMcpServerID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := inventory.Fingerprint()
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "inventory cannot be frozen")
	}
	tools := make([]*gen.GatewayReviewedTool, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		var definition bytes.Buffer
		if err := json.Indent(&definition, tool.Definition, "", "  "); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid reviewed definition")
		}
		tools = append(tools, &gen.GatewayReviewedTool{Name: tool.Name, Fingerprint: tool.Fingerprint, Definition: definition.String()})
	}
	return &gen.GatewayToolsetReview{Fingerprint: fingerprint, Tools: tools}, nil
}
