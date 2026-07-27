package remotemcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type toolVariationLoader interface {
	ListByGroupIDAndToolSource(context.Context, variationsrepo.ListByGroupIDAndToolSourceParams) ([]variationsrepo.ToolVariation, error)
}

// ToolsVariationInterceptor applies configured tool variations to a remote
// MCP's live tools/list response and translates aliases back to upstream names
// on tools/call.
type ToolsVariationInterceptor struct {
	loader    toolVariationLoader
	groupID   uuid.UUID
	projectID uuid.UUID
	identity  proxy.ServerIdentity
}

var (
	_ proxy.ToolsListResponseInterceptor = (*ToolsVariationInterceptor)(nil)
	_ proxy.ToolsCallRequestInterceptor  = (*ToolsVariationInterceptor)(nil)
)

func NewToolsVariationInterceptor(
	loader toolVariationLoader,
	groupID uuid.UUID,
	projectID uuid.UUID,
	identity proxy.ServerIdentity,
) *ToolsVariationInterceptor {
	return &ToolsVariationInterceptor{
		loader:    loader,
		groupID:   groupID,
		projectID: projectID,
		identity:  identity,
	}
}

func (i *ToolsVariationInterceptor) Name() string {
	return "tools-variation"
}

func (i *ToolsVariationInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Result == nil || len(list.Result.Tools) == 0 {
		return nil
	}

	variations, err := i.load(ctx)
	if err != nil {
		return err
	}
	if len(variations) == 0 {
		return nil
	}

	bySourceName := make(map[string]variationsrepo.ToolVariation, len(variations))
	for _, variation := range variations {
		bySourceName[variation.SrcToolUrn.Name] = variation
	}

	for _, tool := range list.Result.Tools {
		if tool == nil {
			continue
		}
		variation, ok := bySourceName[tool.Name]
		if !ok {
			continue
		}
		applyRemoteToolVariation(tool, variation)
	}

	if err := list.SetTools(list.Result.Tools); err != nil {
		return fmt.Errorf("commit varied tools/list result: %w", err)
	}
	return nil
}

func (i *ToolsVariationInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	if call == nil || call.Params == nil {
		return nil
	}

	variations, err := i.load(ctx)
	if err != nil {
		return err
	}

	for _, variation := range variations {
		if variation.Name.Valid && variation.Name.String != "" && variation.Name.String == call.Params.Name {
			if err := call.SetName(variation.SrcToolUrn.Name); err != nil {
				return fmt.Errorf("restore upstream tool name: %w", err)
			}
			return nil
		}
	}
	return nil
}

func (i *ToolsVariationInterceptor) load(ctx context.Context) ([]variationsrepo.ToolVariation, error) {
	variations, err := i.loader.ListByGroupIDAndToolSource(ctx, variationsrepo.ListByGroupIDAndToolSourceParams{
		GroupID:     i.groupID,
		ProjectID:   i.projectID,
		KindValue:   i.identity.ToolURNKind(),
		SourceValue: i.identity.SourceID(),
	})
	if err != nil {
		return nil, fmt.Errorf("load remote MCP tool variations: %w", err)
	}
	return variations, nil
}

func applyRemoteToolVariation(tool *mcp.Tool, variation variationsrepo.ToolVariation) {
	if variation.Name.Valid && variation.Name.String != "" {
		tool.Name = variation.Name.String
	}
	if variation.Description.Valid && variation.Description.String != "" {
		tool.Description = variation.Description.String
	}

	hasAnnotationOverride := (variation.Title.Valid && variation.Title.String != "") ||
		variation.ReadOnlyHint.Valid ||
		variation.DestructiveHint.Valid ||
		variation.IdempotentHint.Valid ||
		variation.OpenWorldHint.Valid
	if !hasAnnotationOverride {
		return
	}
	if tool.Annotations == nil {
		tool.Annotations = &mcp.ToolAnnotations{
			DestructiveHint: nil,
			IdempotentHint:  false,
			OpenWorldHint:   nil,
			ReadOnlyHint:    false,
			Title:           "",
		}
	}
	if variation.Title.Valid && variation.Title.String != "" {
		tool.Annotations.Title = variation.Title.String
	}
	if variation.ReadOnlyHint.Valid {
		tool.Annotations.ReadOnlyHint = variation.ReadOnlyHint.Bool
	}
	if variation.DestructiveHint.Valid {
		tool.Annotations.DestructiveHint = &variation.DestructiveHint.Bool
	}
	if variation.IdempotentHint.Valid {
		tool.Annotations.IdempotentHint = variation.IdempotentHint.Bool
	}
	if variation.OpenWorldHint.Valid {
		tool.Annotations.OpenWorldHint = &variation.OpenWorldHint.Bool
	}
}
