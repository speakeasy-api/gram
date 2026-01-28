package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
)

type EnvironmentLoader interface {
	// Load retrieves the environment variables for a given project and environment slug or ID.
	//
	// # Errors
	//   * [ErrNotFound]: when the environment does not exist.
	//   * `error`: when an unrecognized error occurs.
	Load(ctx context.Context, projectID uuid.UUID, environmentID SlugOrID) (map[string]string, error)

	// LoadSystemEnv loads and merges source, toolset, and attached environments.
	// Merges in order: source env (base) -> toolset env -> attached env (highest priority).
	// Returns empty map if no environments exist.
	//
	// # Errors
	//   * `error`: when an unrecognized error occurs.
	LoadSystemEnv(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID, sourceKind string, sourceSlug string) (*CaseInsensitiveEnv, error)
}

type SlugOrID struct {
	ID   uuid.UUID
	Slug string
}

var NilSlugOrID = SlugOrID{
	ID:   uuid.Nil,
	Slug: "",
}

func ID(id uuid.UUID) SlugOrID {
	return SlugOrID{
		ID:   id,
		Slug: "",
	}
}

func Slug(slug string) SlugOrID {
	return SlugOrID{
		Slug: slug,
		ID:   uuid.Nil,
	}
}

func (s *SlugOrID) String() string {
	if s.ID != uuid.Nil {
		return fmt.Sprintf("UUID(%s)", s.ID)
	}

	return fmt.Sprintf("Slug(%q)", s.Slug)
}

func (s *SlugOrID) IsEmpty() bool {
	return s.ID == uuid.Nil && s.Slug == ""
}

type CaseInsensitiveEnv struct {
	data map[string]string
}

func NewCaseInsensitiveEnv() *CaseInsensitiveEnv {
	return &CaseInsensitiveEnv{data: make(map[string]string)}
}

func CIEnvFrom(vars map[string]string) *CaseInsensitiveEnv {
	env := NewCaseInsensitiveEnv()
	for k, v := range vars {
		env.Set(k, v)
	}
	return env
}

func (c *CaseInsensitiveEnv) Get(key string) string {
	return c.data[strings.ToLower(key)]
}

func (c *CaseInsensitiveEnv) Set(key, value string) {
	c.data[strings.ToLower(key)] = value
}

func (c *CaseInsensitiveEnv) All() map[string]string {
	result := make(map[string]string, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

type ToolCallEnv struct {
	SystemEnv  *CaseInsensitiveEnv
	UserConfig *CaseInsensitiveEnv
}

func (e ToolCallEnv) FilterOmittedEnvVars(
	ctx context.Context,
	repo *mcpmetadata_repo.Queries,
	toolsetID uuid.UUID,
) error {
	rawMetadata, err := repo.GetMetadataForToolset(ctx, toolsetID)
	if err != nil {
		// Fallback behavior for backwards compatibility
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	mcpMetadata, err := mcpmetadata.ToMCPMetadata(ctx, repo, rawMetadata)
	if err != nil {
		return err
	}

	// Fallback behavior for backwards compatibility
	if mcpMetadata.DefaultEnvironmentID == nil {
		return nil
	}

	systemKeysToRetain := make(map[string]bool)
	userKeysToRetain := make(map[string]bool)
	for _, config := range mcpMetadata.EnvironmentConfigs {
		if config.ProvidedBy == "system" {
			systemKeysToRetain[strings.ToLower(config.VariableName)] = true
		} else if config.ProvidedBy == "user" {
			userKeysToRetain[strings.ToLower(config.VariableName)] = true
		}
	}

	// Delete environment variables that are not meant to be sourced from the given location
	// E.G. if a variable is set to "system" it should not be sourced from the user config
	// Removes anything not explicitly listed as a system or user variable
	for key := range e.SystemEnv.data {
		if !systemKeysToRetain[key] {
			delete(e.SystemEnv.data, key)
		}
	}
	for key := range e.UserConfig.data {
		if !userKeysToRetain[key] {
			delete(e.UserConfig.data, key)
		}
	}

	return nil
}
