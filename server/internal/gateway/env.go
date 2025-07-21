package gateway

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type EnvironmentLoader interface {
	// Load retrieves the environment variables for a given project and environment slug or ID.
	//
	// # Errors
	//   * [ErrNotFound]: when the environment does not exist.
	//   * `error`: when an unrecognized error occurs.
	Load(ctx context.Context, projectID uuid.UUID, environmentID SlugOrID) (map[string]string, error)
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
