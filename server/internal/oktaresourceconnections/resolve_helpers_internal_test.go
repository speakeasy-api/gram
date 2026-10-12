package oktaresourceconnections

import (
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// allClients applies one resource scope set to every client, as if each owned the resource.
func allClients(rs remotesessions.ResourceScopes) func(uuid.UUID) remotesessions.ResourceScopes {
	return func(uuid.UUID) remotesessions.ResourceScopes { return rs }
}
