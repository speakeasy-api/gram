package users

import (
	"context"

	"github.com/speakeasy-api/gram/server/gen/organizations"
)

// OrganizationsService is the subset of the organizations management service
// that the managed assistant's user-directory tools call. The concrete
// organizations service satisfies it; tools pass nil auth tokens because the
// assistant runtime supplies auth context out of band.
type OrganizationsService interface {
	ListUsers(ctx context.Context, payload *organizations.ListUsersPayload) (*organizations.ListUsersResult, error)
}
