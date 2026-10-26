package platformmcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// pluginMemberAliases loads the legacy alias of each hosted member that has no endpoint slug.
func pluginMemberAliases[T any](ctx context.Context, db platformrepo.DBTX, projectID uuid.UUID, rows []T, slugOf func(T) (string, uuid.NullUUID)) (map[uuid.UUID]hostingstate.Address, error) {
	var ids []uuid.UUID
	for _, row := range rows {
		if slug, toolsetID := slugOf(row); slug == "" && toolsetID.Valid {
			ids = append(ids, toolsetID.UUID)
		}
	}
	return legacyalias.List(ctx, db, projectID, ids) //nolint:wrapcheck // legacyalias names the failing query.
}

// memberSlug is a member's endpoint slug, else its hosted toolset's legacy alias.
func memberSlug(slug string, toolsetID uuid.NullUUID, aliases map[uuid.UUID]hostingstate.Address) string {
	if slug != "" || !toolsetID.Valid {
		return slug
	}
	return aliases[toolsetID.UUID].Slug.String
}
