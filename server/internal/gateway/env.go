package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type EnvironmentLoader interface {
	// Load retrieves the environment variables for a given project and environment slug or ID.
	//
	// # Errors
	//   * [ErrNotFound]: when the environment does not exist.
	//   * `error`: when an unrecognized error occurs.
	Load(ctx context.Context, projectID uuid.UUID, environmentID SlugOrID) (map[string]string, error)

	// LoadSystemEnv loads and merges source and toolset environments.
	// Merges in order: source env (base) -> toolset env (override).
	// Returns empty map if neither environment exists.
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
	// UserAgent is a custom User-Agent header to send with HTTP requests
	UserAgent string
}
