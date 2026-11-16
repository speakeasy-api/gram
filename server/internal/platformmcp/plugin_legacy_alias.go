package platformmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// pluginMemberAliases loads the legacy alias of each hosted member that has no endpoint slug.
func pluginMemberAliases[T any](ctx context.Context, db platformrepo.DBTX, projectID uuid.UUID, rows []T, slugOf func(T) (string, uuid.NullUUID)) (map[uuid.UUID]legacyalias.Alias, error) {
	var ids []uuid.UUID
	for _, row := range rows {
		if slug, toolsetID := slugOf(row); slug == "" && toolsetID.Valid {
			ids = append(ids, toolsetID.UUID)
		}
	}
	aliases, err := legacyalias.List(ctx, db, projectID, ids)
	if err != nil {
		return nil, fmt.Errorf("load plugin member aliases: %w", err)
	}
	return aliases, nil
}

// memberSlug is a member's endpoint slug, else its hosted toolset's legacy alias.
func memberSlug(slug string, toolsetID uuid.NullUUID, aliases map[uuid.UUID]legacyalias.Alias) string {
	if slug != "" || !toolsetID.Valid {
		return slug
	}
	return aliases[toolsetID.UUID].Slug.String
}
