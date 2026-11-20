package mcpfeedback_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
)

func TestSettingsToolUsesTheDefaultsUntilAnAdminReplacesThem(t *testing.T) {
	t.Parallel()

	defaults := mcpfeedback.Settings{Enabled: true, ToolName: "", ToolDescription: ""}.Tool()
	require.Equal(t, mcpfeedback.DefaultToolName, defaults.Name)
	require.Equal(t, mcpfeedback.ToolTitle, defaults.Title)
	require.Equal(t, mcpfeedback.DefaultToolDescription, defaults.Description)

	custom := mcpfeedback.Settings{Enabled: true, ToolName: "acme_submit_feedback", ToolDescription: "Tell Acme what went wrong."}.Tool()
	require.Equal(t, "acme_submit_feedback", custom.Name)
	require.Equal(t, "Tell Acme what went wrong.", custom.Description)
	require.JSONEq(t, string(defaults.InputSchema), string(custom.InputSchema))
}

// The schema an agent reads must state the limits ParseReport enforces.
func TestToolInputSchemaStatesTheEnforcedLimits(t *testing.T) {
	t.Parallel()

	var schema struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type      string   `json:"type"`
			MinLength int      `json:"minLength"`
			MaxLength int      `json:"maxLength"`
			Enum      []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	tool := mcpfeedback.Settings{Enabled: true, ToolName: "", ToolDescription: ""}.Tool()
	require.NoError(t, json.Unmarshal(tool.InputSchema, &schema))

	require.Equal(t, "object", schema.Type)
	require.Equal(t, []string{"content"}, schema.Required)
	require.Len(t, schema.Properties, 6)
	require.Equal(t, 1, schema.Properties["content"].MinLength)
	require.Equal(t, mcpfeedback.MaxContentLength, schema.Properties["content"].MaxLength)
	require.Equal(t, mcpfeedback.MaxWorkaroundLength, schema.Properties["workaround"].MaxLength)
	require.Equal(t, mcpfeedback.MaxIntentionLength, schema.Properties["intention"].MaxLength)
	require.Equal(t, mcpfeedback.MaxReportedToolNameLength, schema.Properties["tool"].MaxLength)
	require.Equal(t, mcpfeedback.MaxModelLength, schema.Properties["model"].MaxLength)
	require.Equal(t, []string{"blocked", "degraded", "cosmetic"}, schema.Properties["severity"].Enum)

	// Properties are listed in the order an agent should fill them in.
	order := []string{`"content"`, `"severity"`, `"workaround"`, `"intention"`, `"tool"`, `"model"`}
	last := -1
	for _, name := range order {
		at := strings.Index(string(tool.InputSchema), name+`:{`)
		require.Greater(t, at, last, name)
		last = at
	}
}

func TestDefaultToolDescriptionFitsItsOwnLimit(t *testing.T) {
	t.Parallel()

	require.LessOrEqual(t, utf8.RuneCountInString(mcpfeedback.DefaultToolDescription), mcpfeedback.MaxToolDescriptionLength)
	require.NoError(t, mcpfeedback.Settings{Enabled: true, ToolName: mcpfeedback.DefaultToolName, ToolDescription: mcpfeedback.DefaultToolDescription}.Validate())
}

func TestSettingsValidate(t *testing.T) {
	t.Parallel()

	valid := []mcpfeedback.Settings{
		{Enabled: false, ToolName: "", ToolDescription: ""},
		{Enabled: true, ToolName: "acme-Submit_feedback2", ToolDescription: "Short."},
		{Enabled: true, ToolName: strings.Repeat("a", mcpfeedback.MaxToolNameLength), ToolDescription: strings.Repeat("é", mcpfeedback.MaxToolDescriptionLength)},
	}
	for i, settings := range valid {
		t.Run(fmt.Sprintf("valid %d", i), func(t *testing.T) {
			t.Parallel()
			require.NoError(t, settings.Validate())
		})
	}

	invalid := map[string]mcpfeedback.Settings{
		"tool name must be at most 64 characters":                             {Enabled: true, ToolName: strings.Repeat("a", mcpfeedback.MaxToolNameLength+1), ToolDescription: ""},
		"tool name may only contain letters, digits, underscores and hyphens": {Enabled: true, ToolName: "submit feedback", ToolDescription: ""},
		"tool description must be at most 2000 characters":                    {Enabled: true, ToolName: "", ToolDescription: strings.Repeat("é", mcpfeedback.MaxToolDescriptionLength+1)},
		"tool name execute_tool is already used by Speakeasy on every server": {Enabled: true, ToolName: "execute_tool", ToolDescription: ""},
	}
	for message, settings := range invalid {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			require.EqualError(t, settings.Validate(), message)
		})
	}

	for _, name := range append([]string{"submit.feedback", "submit/feedback", "sübmit", "submit_feedback!"}, mcpfeedback.ReservedToolNames...) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, mcpfeedback.Settings{Enabled: true, ToolName: name, ToolDescription: ""}.Validate())
		})
	}
}
