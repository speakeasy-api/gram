package platformmcp

import (
	"crypto/hmac"

	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
// user and surface instead — otherwise pagination would fail outright rather
// than merely being unbound.
func principalCursorBinding(principal Principal) string {
	if principal.Generation != "" {
		return principal.Generation
	}
	if principal.UserID == "" {
		return ""
	}
	return string(principal.surface()) + ":" + userSubjectURN(principal.UserID)
}

type catalogCursorCodec struct {
	key []byte
}

func newCatalogCursorCodec(keyMaterial string) (*catalogCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrCatalogCursorInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-catalog-cursor:" + keyMaterial))
	return &catalogCursorCodec{key: key[:]}, nil
}

func (c *catalogCursorCodec) Encode(cursor catalogCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.OrganizationID == "" || cursor.Generation == "" || cursor.Position < 0 {
		return "", ErrCatalogCursorInvalid
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode platform mcp catalog cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (c *catalogCursorCodec) Decode(value string, principal Principal, query, providerKey string) (int, error) {
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || binding == "" {
		return 0, ErrCatalogCursorInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return 0, ErrCatalogCursorInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, ErrCatalogCursorInvalid
	}
	var cursor catalogCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Position < 0 || cursor.OrganizationID != principal.OrganizationID || cursor.Generation != binding || cursor.Query != normalizeCatalogQuery(query) || cursor.ProviderKey != normalizeCatalogProviderKey(providerKey) {
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
