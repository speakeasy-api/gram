package authz

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
)

// PinAuthClientRestrictionTypes returns grants with the client type written
// into each mcp:blocked_auth restriction that names one auth client and leaves
// the type open. Runtime checks always carry the client's type, so the pinned
// restriction withdraws exactly what it did. Containment compares selectors
// without looking clients up, and an open type makes a block on one shared
// credential look as if it could reach personal connections too, which stops
// anyone holding such a block from delegating the personal default. A
// restriction naming an unknown client is left as it is.
func PinAuthClientRestrictionTypes(ctx context.Context, db accessrepo.DBTX, organizationID string, grants []Grant) ([]Grant, error) {
	var ids []uuid.UUID
	for _, grant := range grants {
		if id, ok := unpinnedAuthClientRestriction(grant); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return grants, nil
	}

	rows, err := accessrepo.New(db).ListAuthClientTypes(ctx, accessrepo.ListAuthClientTypesParams{OrganizationID: organizationID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("list auth client types: %w", err)
	}
	clientTypes := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		clientTypes[row.ID] = row.ClientType
	}

	pinned := make([]Grant, len(grants))
	for i, grant := range grants {
		pinned[i] = grant
		id, ok := unpinnedAuthClientRestriction(grant)
		if !ok {
			continue
		}
		clientType, known := clientTypes[id]
		if !known {
			continue
		}
		pinned[i].Selector = maps.Clone(grant.Selector)
		pinned[i].Selector[SelectorKeyClientType] = clientType
	}
	return pinned, nil
}

func unpinnedAuthClientRestriction(grant Grant) (uuid.UUID, bool) {
	if grant.Scope != ScopeMCPBlockedAuth {
		return uuid.Nil, false
	}
	if clientType := grant.Selector[SelectorKeyClientType]; clientType != "" && clientType != WildcardResource {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(grant.Selector[SelectorKeyResourceID])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
