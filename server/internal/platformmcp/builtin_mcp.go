package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
)

// BuiltinMCPManagement shares dashboard checks for live human administrator
// membership, project ownership, concurrency and atomic audit.
type BuiltinMCPManagement interface {
	List(context.Context, string, uuid.UUID) ([]builtinmcp.Server, error)
	Get(context.Context, string, uuid.UUID, string) (builtinmcp.Server, error)
	Update(context.Context, string, uuid.UUID, string, builtinmcp.Update) (builtinmcp.Server, error)
}
type ListBuiltinMCPsInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID; no default project is inferred"`
}
type GetBuiltinMCPInput struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key" jsonschema:"immutable built-in catalog key, for example slack"`
}
type ConfigureBuiltinMCPInput struct {
	ProjectID       string  `json:"project_id"`
	Key             string  `json:"key"`
	Hidden          bool    `json:"hidden" jsonschema:"visual discovery preference only; not an authorization or runtime control"`
	EnvironmentID   *string `json:"environment_id,omitempty" jsonschema:"project environment UUID; omit to clear the override and use the project default"`
	ExpectedVersion string  `json:"expected_version" jsonschema:"exact version from a fresh get_builtin_mcp result"`
	Confirmed       bool    `json:"confirmed" jsonschema:"true only after explicit user confirmation of these exact settings"`
}
type ListBuiltinMCPsOutput struct {
	Servers []builtinmcp.Server `json:"servers"`
}

var errBuiltinMCPInvalid = errors.New("provide an explicit project, exact built-in key, fresh version and confirmed settings")

func (r *PostgresReader) WithBuiltinMCPs(core BuiltinMCPManagement) *PostgresReader {
	if r != nil {
		r.builtinMCPs = core
	}
	return r
}
func builtinMCPProject(principal Principal, project string) (uuid.UUID, error) {
	id, err := uuid.Parse(project)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errBuiltinMCPInvalid
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return uuid.Nil, ErrUnavailable
	}
	return id, nil
}
func configureBuiltinMCP(ctx context.Context, core BuiltinMCPManagement, principal Principal, input ConfigureBuiltinMCPInput) (builtinmcp.Server, error) {
	if core == nil {
		return builtinmcp.Server{}, ErrUnavailable
	}
	project, err := builtinMCPProject(principal, input.ProjectID)
	if err != nil {
		return builtinmcp.Server{}, err
	}
	if input.Key == "" || input.ExpectedVersion == "" || !input.Confirmed {
		return builtinmcp.Server{}, errBuiltinMCPInvalid
	}
	environment := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.EnvironmentID != nil {
		id, err := uuid.Parse(*input.EnvironmentID)
		if err != nil || id == uuid.Nil {
			return builtinmcp.Server{}, errBuiltinMCPInvalid
		}
		environment = uuid.NullUUID{UUID: id, Valid: true}
	}
	result, err := core.Update(ctx, principal.OrganizationID, project, input.Key, builtinmcp.Update{Hidden: input.Hidden, EnvironmentID: environment, ExpectedVersion: input.ExpectedVersion})
	if err != nil {
		return builtinmcp.Server{}, fmt.Errorf("configure built-in MCP: %w", err)
	}
	return result, nil
}
