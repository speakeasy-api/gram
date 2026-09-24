package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// pluginCursor is one page boundary in a project's plugin inventory. It is
// signed and bound to the principal and project that issued it, so a cursor
// cannot be replayed against another organization's listing.
type pluginCursor struct {
	OrganizationID string `json:"organization_id"`
	Binding        string `json:"binding"`
	ProjectID      string `json:"project_id"`
	AfterPluginID  string `json:"after_plugin_id"`
}

type pluginCursorCodec struct {
	key []byte
}

func newPluginCursorCodec(keyMaterial string) (*pluginCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrPluginCursorInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-plugin-cursor:" + keyMaterial))
	return &pluginCursorCodec{key: key[:]}, nil
}

func (c *pluginCursorCodec) Encode(cursor pluginCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.OrganizationID == "" || cursor.Binding == "" || cursor.ProjectID == "" || cursor.AfterPluginID == "" {
		return "", ErrPluginCursorInvalid
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode Platform MCP plugin cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Decode returns the plugin id a page resumes after. An empty cursor is the
// first page, not an error.
func (c *pluginCursorCodec) Decode(value string, principal Principal, projectID uuid.UUID) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || principal.OrganizationID == "" || binding == "" || projectID == uuid.Nil {
		return uuid.Nil, ErrPluginCursorInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return uuid.Nil, ErrPluginCursorInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return uuid.Nil, ErrPluginCursorInvalid
	}
	var cursor pluginCursor
	if err := json.Unmarshal(payload, &cursor); err != nil ||
		cursor.OrganizationID != principal.OrganizationID ||
		cursor.Binding != binding ||
		cursor.ProjectID != projectID.String() {
		return uuid.Nil, ErrPluginCursorInvalid
	}
	after, err := uuid.Parse(cursor.AfterPluginID)
	if err != nil {
		return uuid.Nil, ErrPluginCursorInvalid
	}
	return after, nil
}
