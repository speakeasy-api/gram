package mcp

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
)

var (
	errToolsetNotFound = errors.New("toolset not found")
)

// ToolMode specifies whether tools should be loaded statically or dynamically.
type ToolMode string

const (
	ToolModeStatic  ToolMode = "static"
	ToolModeDynamic ToolMode = "dynamic"
)

// McpInputs contains context for MCP request execution.
// This type is exported for use by internal clients (e.g., agent workflows).
type McpInputs struct {
	ProjectID        uuid.UUID
	Toolset          string
	Environment      string
	McpEnvVariables  map[string]string
	OauthTokenInputs []OauthTokenInputs
	Authenticated    bool
	SessionID        string
	ChatID           string
	Mode             ToolMode
	UserID           string
	ExternalUserID   string
	APIKeyID         string
}

// OauthTokenInputs contains OAuth token information for MCP requests.
type OauthTokenInputs struct {
	SecurityKeys []string // can be empty if a single token applies to the whole server
	Token        string
}

// ToolListResult represents the result of listing tools from an MCP server.
type ToolListResult struct {
	Tools []ToolListEntry
}

// ToolListEntry represents a single tool in the tool list.
type ToolListEntry struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Annotations *externalmcp.ToolAnnotations
	Meta        map[string]any
}

// ToolCallResult represents the result of calling a tool.
type ToolCallResult struct {
	Content []ContentChunk
	IsError bool
}

// ContentChunk represents one piece of tool output.
type ContentChunk struct {
	Type     string // "text", "image", "audio", "resource"
	Text     string
	Data     string // base64 for binary content
	MimeType string
}

// Convert exported McpInputs to internal mcpInputs
func (m *McpInputs) toInternal() *mcpInputs {
	oauthInputs := make([]oauthTokenInputs, len(m.OauthTokenInputs))
	for i, ot := range m.OauthTokenInputs {
		oauthInputs[i] = oauthTokenInputs{
			securityKeys: ot.SecurityKeys,
			Token:        ot.Token,
		}
	}

	return &mcpInputs{
		projectID:        m.ProjectID,
		toolset:          m.Toolset,
		environment:      m.Environment,
		mcpEnvVariables:  m.McpEnvVariables,
		oauthTokenInputs: oauthInputs,
		authenticated:    m.Authenticated,
		sessionID:        m.SessionID,
		chatID:           m.ChatID,
		mode:             m.Mode,
		userID:           m.UserID,
		externalUserID:   m.ExternalUserID,
		apiKeyID:         m.APIKeyID,
	}
}
