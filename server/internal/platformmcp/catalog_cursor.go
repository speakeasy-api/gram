package platformmcp

import (
	"errors"
	"fmt"
	"strings"
)

const catalogPageSize = 20

var ErrCatalogCursorInvalid = errors.New("invalid platform mcp catalog cursor")

type catalogCursor struct {
	OrganizationID string `json:"organization_id"`
	// Generation binds the cursor to the caller's session, so a paginated walk
	// cannot be resumed by a different one. See principalCursorBinding.
	Generation  string `json:"generation"`
	Query       string `json:"query"`
	ProviderKey string `json:"provider_key"`
	Position    int    `json:"position"`
}

// principalCursorBinding is the session a cursor belongs to. An OAuth caller's
// session is its connection generation, which changes on reauthorization. A
// connection-less caller has no generation, so its cursors bind to the acting
// surface and subject instead — otherwise pagination would fail outright rather
// than merely being unbound. It uses the same HasConnection predicate as the
// operation budget, so a caller is never classified as connected in one place
// and connection-less in the other.
func principalCursorBinding(principal Principal) string {
	if principal.HasConnection() {
		return principal.Generation
	}
	if principal.UserID == "" {
		return ""
	}
	return string(principal.surface()) + ":" + userSubjectURN(principal.UserID)
}

type catalogCursorCodec struct {
	key signedCursorKey
}

func newCatalogCursorCodec(keyMaterial string) (*catalogCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrCatalogCursorInvalid
	}
	return &catalogCursorCodec{key: newSignedCursorKey("platform-mcp-catalog-cursor", keyMaterial)}, nil
}

func (c *catalogCursorCodec) Encode(cursor catalogCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.OrganizationID == "" || cursor.Generation == "" || cursor.Position < 0 {
		return "", ErrCatalogCursorInvalid
	}
	token, err := sealCursor(c.key, cursor)
	if err != nil {
		return "", fmt.Errorf("encode platform mcp catalog cursor: %w", err)
	}
	return token, nil
}

func (c *catalogCursorCodec) Decode(value string, principal Principal, query, providerKey string) (int, error) {
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || binding == "" {
		return 0, ErrCatalogCursorInvalid
	}
	cursor, ok := openCursor[catalogCursor](c.key, value)
	if !ok || cursor.Position < 0 || cursor.OrganizationID != principal.OrganizationID || cursor.Generation != binding || cursor.Query != normalizeCatalogQuery(query) || cursor.ProviderKey != normalizeCatalogProviderKey(providerKey) {
		return 0, ErrCatalogCursorInvalid
	}
	return cursor.Position, nil
}

func normalizeCatalogQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func normalizeCatalogProviderKey(providerKey string) string {
	return strings.ToLower(strings.TrimSpace(providerKey))
}
