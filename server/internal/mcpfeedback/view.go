package mcpfeedback

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
)

// The views are built here rather than in the mv package because they need
// the tool's defaults, which live in this package, and this package already
// reaches mv through its middleware import, so mv could not import it back.

// buildSettingsView describes a server's feedback tool settings. updatedAt
// and updatedByEmail are nil for a server that never had settings stored.
func buildSettingsView(mcpServerID uuid.UUID, settings Settings, updatedAt *time.Time, updatedByEmail *string) *types.McpFeedbackSettings {
	tool := settings.Tool()

	var updatedAtText *string
	if updatedAt != nil {
		updatedAtText = new(updatedAt.UTC().Format(time.RFC3339Nano))
	}

	return &types.McpFeedbackSettings{
		McpServerID:            mcpServerID.String(),
		Enabled:                settings.Enabled,
		ToolName:               conv.PtrEmpty(settings.ToolName),
		ToolDescription:        conv.PtrEmpty(settings.ToolDescription),
		DefaultToolName:        DefaultToolName,
		DefaultToolDescription: DefaultToolDescription,
		Tool: &types.McpFeedbackTool{
			Name:        tool.Name,
			Title:       tool.Title,
			Description: tool.Description,
			InputSchema: string(tool.InputSchema),
		},
		UpdatedAt:      updatedAtText,
		UpdatedByEmail: updatedByEmail,
		Version:        settingsVersion(updatedAt),
	}
}

// settingsVersion identifies a server's stored settings: the time they were
// last changed, to the microsecond the database keeps, or empty when there
// are none. Callers treat it as opaque and send it back unchanged.
func settingsVersion(updatedAt *time.Time) string {
	if updatedAt == nil {
		return ""
	}

	return updatedAt.UTC().Format(time.RFC3339Nano)
}

// buildFeedbackView describes one stored report.
func buildFeedbackView(row repo.ListFeedbackRow) *types.McpFeedback {
	return &types.McpFeedback{
		ID:             row.ID.String(),
		McpServerID:    row.McpServerID.String(),
		Content:        row.Content,
		Severity:       conv.FromPGText[types.McpFeedbackSeverity](row.Severity),
		Workaround:     conv.FromPGText[string](row.Workaround),
		Intention:      conv.FromPGText[string](row.Intention),
		ToolName:       conv.FromPGText[string](row.ToolName),
		Model:          conv.FromPGText[string](row.Model),
		UserID:         conv.FromPGText[string](row.UserID),
		UserEmail:      conv.FromPGText[string](row.UserEmail),
		ExternalUserID: conv.FromPGText[string](row.ExternalUserID),
		ClientName:     conv.FromPGText[string](row.ClientName),
		ClientVersion:  conv.FromPGText[string](row.ClientVersion),
		CreatedAt:      row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
	}
}
