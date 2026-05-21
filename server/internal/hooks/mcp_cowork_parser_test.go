package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCoworkMCPInventory(t *testing.T) {
	t.Parallel()

	t.Run("populated entry", func(t *testing.T) {
		t.Parallel()
		raw := []any{
			map[string]any{
				"name":           "Slack",
				"url":            "https://mcp.example.com/slack",
				"connector_uuid": "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
			},
		}
		got := ParseCoworkMCPInventory(raw)
		if assert.Len(t, got, 1) {
			assert.Equal(t, "Slack", got[0].Name)
			assert.Equal(t, "https://mcp.example.com/slack", got[0].URL)
			assert.Equal(t, "a1b2c3d4-e5f6-7890-abcd-ef0123456789", got[0].ConnectorUUID)
			assert.Equal(t, "claude.ai", got[0].Source)
			assert.Equal(t, "HTTP", got[0].Transport)
		}
	})

	t.Run("missing name but uuid present is kept", func(t *testing.T) {
		t.Parallel()
		// The fallback path: even without a Name, the entry is still
		// matchable by connector UUID and contributes its URL to
		// telemetry — only the source-name override is skipped.
		raw := []any{
			map[string]any{
				"url":            "https://mcp.example.com/slack",
				"connector_uuid": "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
			},
		}
		got := ParseCoworkMCPInventory(raw)
		if assert.Len(t, got, 1) {
			assert.Empty(t, got[0].Name)
			assert.Equal(t, "a1b2c3d4-e5f6-7890-abcd-ef0123456789", got[0].ConnectorUUID)
		}
	})

	t.Run("missing both name and uuid is dropped", func(t *testing.T) {
		t.Parallel()
		raw := []any{
			map[string]any{"url": "https://mcp.example.com/slack"},
		}
		assert.Empty(t, ParseCoworkMCPInventory(raw))
	})

	t.Run("non-array input returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, ParseCoworkMCPInventory("not-an-array"))
		assert.Nil(t, ParseCoworkMCPInventory(nil))
	})
}
