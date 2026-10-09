package platformmcp

import (
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
	key signedCursorKey
}

func newPluginCursorCodec(keyMaterial string) (*pluginCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrPluginCursorInvalid
	}
	return &pluginCursorCodec{key: newSignedCursorKey("platform-mcp-plugin-cursor", keyMaterial)}, nil
}

func (c *pluginCursorCodec) Encode(cursor pluginCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.OrganizationID == "" || cursor.Binding == "" || cursor.ProjectID == "" || cursor.AfterPluginID == "" {
		return "", ErrPluginCursorInvalid
	}
	token, err := sealCursor(c.key, cursor)
	if err != nil {
		return "", fmt.Errorf("encode Platform MCP plugin cursor: %w", err)
	}
	return token, nil
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
	cursor, ok := openCursor[pluginCursor](c.key, value)
	if !ok ||
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
