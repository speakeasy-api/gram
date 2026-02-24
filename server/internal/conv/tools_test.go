package conv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestGetToolURN_AgentDefinition(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		HTTPToolDefinition:        nil,
		FunctionToolDefinition:    nil,
		PromptTemplate:            nil,
		ExternalMcpToolDefinition: nil,
		AgentDefinition: &types.AgentDefinition{
			ID:           "agent-id-1",
			ProjectID:    "proj-1",
			ToolUrn:      "tools:agent:agent:research-agent",
			Name:         "research-agent",
			Description:  "A research agent",
			Title:        nil,
			Instructions: "Research things",
			Tools:        nil,
			Model:        nil,
			Annotations:  nil,
			CreatedAt:    "2025-01-01T00:00:00Z",
			UpdatedAt:    "2025-01-01T00:00:00Z",
		},
	}

	urn, err := conv.GetToolURN(tool)
	require.NoError(t, err)
	require.NotNil(t, urn)
	require.Equal(t, "tools:agent:agent:research-agent", urn.String())
}

func TestGetToolURN_AgentDefinition_Invalid(t *testing.T) {
	t.Parallel()

	tool := types.Tool{
		HTTPToolDefinition:        nil,
		FunctionToolDefinition:    nil,
		PromptTemplate:            nil,
		ExternalMcpToolDefinition: nil,
		AgentDefinition: &types.AgentDefinition{
			ID:           "agent-id-1",
			ProjectID:    "proj-1",
			ToolUrn:      "invalid-urn",
			Name:         "bad-agent",
			Description:  "bad",
			Title:        nil,
			Instructions: "bad",
			Tools:        nil,
			Model:        nil,
			Annotations:  nil,
			CreatedAt:    "2025-01-01T00:00:00Z",
			UpdatedAt:    "2025-01-01T00:00:00Z",
		},
	}

	_, err := conv.GetToolURN(tool)
	require.Error(t, err)
}

func TestToBaseTool_AgentDefinition(t *testing.T) {
	t.Parallel()

	readOnly := true
	annotations := &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	}

	tool := &types.Tool{
		HTTPToolDefinition:        nil,
		FunctionToolDefinition:    nil,
		PromptTemplate:            nil,
		ExternalMcpToolDefinition: nil,
		AgentDefinition: &types.AgentDefinition{
			ID:           "agent-id-1",
			ProjectID:    "proj-1",
			ToolUrn:      "tools:agent:agent:research-agent",
			Name:         "research-agent",
			Description:  "A research agent",
			Title:        nil,
			Instructions: "Research things",
			Tools:        nil,
			Model:        nil,
			Annotations:  annotations,
			CreatedAt:    "2025-01-01T00:00:00Z",
			UpdatedAt:    "2025-01-01T00:00:00Z",
		},
	}

	base, err := conv.ToBaseTool(tool)
	require.NoError(t, err)
	require.Equal(t, "agent-id-1", base.ID)
	require.Equal(t, "tools:agent:agent:research-agent", base.ToolUrn)
	require.Equal(t, "proj-1", base.ProjectID)
	require.Equal(t, "research-agent", base.Name)
	require.Equal(t, "research-agent", base.CanonicalName)
	require.Equal(t, "A research agent", base.Description)
	require.JSONEq(t, constants.DefaultEmptyToolSchema, base.Schema)
	require.Nil(t, base.SchemaVersion)
	require.Nil(t, base.Confirm)
	require.Nil(t, base.ConfirmPrompt)
	require.Nil(t, base.Summarizer)
	require.Nil(t, base.Canonical)
	require.Nil(t, base.Variation)
	require.NotNil(t, base.Annotations)
	require.Equal(t, &readOnly, base.Annotations.ReadOnlyHint)
	require.Equal(t, "2025-01-01T00:00:00Z", base.CreatedAt)
	require.Equal(t, "2025-01-01T00:00:00Z", base.UpdatedAt)
}

func TestApplyVariation_AgentDefinition(t *testing.T) {
	t.Parallel()

	overrideName := "overridden-name"
	overrideDesc := "Overridden description"
	overrideTitle := "Override Title"
	readOnly := true

	tool := types.Tool{
		HTTPToolDefinition:        nil,
		FunctionToolDefinition:    nil,
		PromptTemplate:            nil,
		ExternalMcpToolDefinition: nil,
		AgentDefinition: &types.AgentDefinition{
			ID:           "agent-id-1",
			ProjectID:    "proj-1",
			ToolUrn:      "tools:agent:agent:original-agent",
			Name:         "original-agent",
			Description:  "Original description",
			Title:        nil,
			Instructions: "Do things",
			Tools:        nil,
			Model:        nil,
			Annotations:  nil,
			CreatedAt:    "2025-01-01T00:00:00Z",
			UpdatedAt:    "2025-01-01T00:00:00Z",
		},
	}

	variation := types.ToolVariation{
		ID:              "var-1",
		GroupID:         "group-1",
		SrcToolUrn:      "tools:agent:agent:original-agent",
		SrcToolName:     "original-agent",
		Confirm:         nil,
		ConfirmPrompt:   nil,
		Name:            &overrideName,
		Description:     &overrideDesc,
		Summarizer:      nil,
		Title:           &overrideTitle,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
		CreatedAt:       "2025-01-02T00:00:00Z",
		UpdatedAt:       "2025-01-02T00:00:00Z",
	}

	conv.ApplyVariation(tool, variation)

	require.Equal(t, overrideName, tool.AgentDefinition.Name)
	require.Equal(t, overrideDesc, tool.AgentDefinition.Description)
	require.NotNil(t, tool.AgentDefinition.Annotations)
	require.Equal(t, &overrideTitle, tool.AgentDefinition.Annotations.Title)
	require.Equal(t, &readOnly, tool.AgentDefinition.Annotations.ReadOnlyHint)
}

func TestToToolListEntry_AgentDefinition(t *testing.T) {
	t.Parallel()

	tool := &types.Tool{
		HTTPToolDefinition:        nil,
		FunctionToolDefinition:    nil,
		PromptTemplate:            nil,
		ExternalMcpToolDefinition: nil,
		AgentDefinition: &types.AgentDefinition{
			ID:           "agent-id-1",
			ProjectID:    "proj-1",
			ToolUrn:      "tools:agent:agent:research-agent",
			Name:         "research-agent",
			Description:  "A research agent",
			Title:        nil,
			Instructions: "Research things",
			Tools:        nil,
			Model:        nil,
			Annotations:  nil,
			CreatedAt:    "2025-01-01T00:00:00Z",
			UpdatedAt:    "2025-01-01T00:00:00Z",
		},
	}

	entry, err := conv.ToToolListEntry(tool)
	require.NoError(t, err)
	require.Equal(t, "research-agent", entry.Name)
	require.Equal(t, "A research agent", entry.Description)
	require.NotNil(t, entry.InputSchema)
	require.JSONEq(t, constants.DefaultEmptyToolSchema, string(entry.InputSchema))
	require.Nil(t, entry.Meta)
}
