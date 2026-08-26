package toolsets

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonschema"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func (s *Service) ListToolSchemaStaticValues(ctx context.Context, payload *gen.ListToolSchemaStaticValuesPayload) (*gen.ListToolSchemaStaticValuesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	toolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      conv.ToLower(string(payload.Slug)),
		ProjectID: *authCtx.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get toolset").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, toolset.ID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	var variationsGroupID *uuid.UUID
	if toolset.ToolVariationsGroupID.Valid {
		variationsGroupID = &toolset.ToolVariationsGroupID.UUID
	}

	described, err := mv.DescribeToolset(
		ctx,
		s.logger,
		s.db,
		mv.ProjectID(*authCtx.ProjectID),
		mv.ToolsetSlug(toolset.Slug),
		&s.toolsetCache,
		variationsGroupID,
	)
	if err != nil {
		return nil, err
	}

	tools := make([]*gen.ToolSchemaStaticValues, 0)
	for _, tool := range described.Tools {
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read tool schema").LogError(ctx, s.logger)
		}

		staticValues, err := jsonschema.StaticValues([]byte(base.Schema))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "inspect tool schema").LogError(ctx, s.logger)
		}
		if len(staticValues) == 0 {
			continue
		}

		values := make([]*gen.ToolSchemaStaticValue, 0, len(staticValues))
		for _, staticValue := range staticValues {
			values = append(values, &gen.ToolSchemaStaticValue{
				SchemaPath: staticValue.SchemaPath,
				Keyword:    staticValue.Keyword,
				ValueJSON:  staticValue.ValueJSON,
			})
		}

		tools = append(tools, &gen.ToolSchemaStaticValues{
			ToolUrn:  base.ToolUrn,
			ToolName: base.Name,
			Values:   values,
		})
	}

	sort.Slice(tools, func(i, j int) bool {
		if tools[i].ToolName != tools[j].ToolName {
			return tools[i].ToolName < tools[j].ToolName
		}
		return tools[i].ToolUrn < tools[j].ToolUrn
	})

	return &gen.ListToolSchemaStaticValuesResult{Tools: tools}, nil
}
