package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type resourcesListResult struct {
	Resources []*resourceListEntry `json:"resources"`
}

type resourceListEntry struct {
	URI         string  `json:"uri"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	MimeType    *string `json:"mimeType,omitempty"`
}

func handleResourcesList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents]) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	resources := make([]*resourceListEntry, 0)

	for _, resource := range toolset.Resources {
		if resource.FunctionResourceDefinition != nil {
			frd := resource.FunctionResourceDefinition
			resources = append(resources, &resourceListEntry{
				URI:         frd.URI,
				Name:        frd.Name,
				Description: &frd.Description,
				MimeType:    frd.MimeType,
			})
		}
	}

	result := &result[resourcesListResult]{
		ID: req.ID,
		Result: resourcesListResult{
			Resources: resources,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/list response").Log(ctx, logger)
	}

	return bs, nil
}
