package projects

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

func (s *Service) WithBuiltinHosts(hosts *orghost.Resolver) *Service {
	s.builtinHosts = hosts
	return s
}

func (s *Service) ListBuiltinMcpServers(ctx context.Context, _ *gen.ListBuiltinMcpServersPayload) (*gen.ListBuiltinMcpServersResult, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	rows, err := builtinmcp.NewStore(s.db, s.audit, s.builtinHosts).List(ctx, ac.ActiveOrganizationID, *ac.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("manage built-in MCP: %w", err)
	}
	result := &gen.ListBuiltinMcpServersResult{Servers: make([]*gen.BuiltinMcpServer, 0, len(rows))}
	for _, row := range rows {
		result.Servers = append(result.Servers, toBuiltinServer(row))
	}
	return result, nil
}

func (s *Service) UpdateBuiltinMcpServer(ctx context.Context, payload *gen.UpdateBuiltinMcpServerPayload) (*gen.BuiltinMcpServer, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	environmentID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.EnvironmentID != nil {
		id, err := uuid.Parse(*payload.EnvironmentID)
		if err != nil || id == uuid.Nil {
			return nil, oops.E(oops.CodeInvalid, nil, "invalid environment ID")
		}
		environmentID = uuid.NullUUID{UUID: id, Valid: true}
	}
	row, err := builtinmcp.NewStore(s.db, s.audit, s.builtinHosts).Update(ctx, ac.ActiveOrganizationID, *ac.ProjectID, payload.Key, builtinmcp.Update{Hidden: payload.Hidden, EnvironmentID: environmentID, ExpectedVersion: payload.ExpectedVersion})
	if err != nil {
		return nil, fmt.Errorf("manage built-in MCP: %w", err)
	}
	return toBuiltinServer(row), nil
}

func toBuiltinServer(row builtinmcp.Server) *gen.BuiltinMcpServer {
	tools := make([]*gen.BuiltinMcpTool, 0, len(row.Tools))
	for _, tool := range row.Tools {
		tools = append(tools, &gen.BuiltinMcpTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return &gen.BuiltinMcpServer{ID: row.ID, Key: row.Key, Name: row.Name, Description: row.Description, URL: row.URL, Hidden: row.Hidden, EnvironmentID: row.EnvironmentID, Version: row.Version, Tools: tools, CredentialVariables: row.CredentialVariables}
}
