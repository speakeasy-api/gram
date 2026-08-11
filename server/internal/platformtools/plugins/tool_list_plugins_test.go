package plugins

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	genplugins "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type stubPluginsService struct {
	payload *genplugins.ListPluginsPayload
	result  *genplugins.ListPluginsResult
}

func (s *stubPluginsService) ListPlugins(_ context.Context, payload *genplugins.ListPluginsPayload) (*genplugins.ListPluginsResult, error) {
	s.payload = payload
	return s.result, nil
}

func testToolCallEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}
}

// The tool projects the management result down to what a caller needs to pick
// a distribution target; servers and assignments are dropped so a project with
// many plugins doesn't flood the model's context.
func TestListPluginsToolReturnsSummariesWithoutServers(t *testing.T) {
	t.Parallel()

	description := "Everything bundle."
	isDefault := false
	serverCount := int64(4)
	skillCount := int64(2)
	assignmentCount := int64(9)

	svc := &stubPluginsService{
		payload: nil,
		result: &genplugins.ListPluginsResult{Plugins: []*genplugins.Plugin{
			{
				ID:              "plugin-id",
				Name:            "Kitchen Sink",
				Slug:            "kitchen-sink",
				Description:     &description,
				IsDefault:       &isDefault,
				ServerCount:     &serverCount,
				SkillCount:      &skillCount,
				AssignmentCount: &assignmentCount,
				Servers: []*genplugins.PluginServer{{
					ID:          "server-id",
					ToolsetID:   nil,
					McpServerID: nil,
					DisplayName: "Some server",
					Policy:      "required",
					SortOrder:   0,
					CreatedAt:   "2026-08-03T00:00:00Z",
				}},
				Assignments: nil,
				CreatedAt:   "2026-08-03T00:00:00Z",
				UpdatedAt:   "2026-08-03T00:00:00Z",
			},
		}},
	}

	var out bytes.Buffer
	require.NoError(t, NewListPluginsTool(svc).Call(t.Context(), testToolCallEnv(), bytes.NewBufferString(`{}`), &out))
	require.Nil(t, svc.payload.SessionToken)
	require.Nil(t, svc.payload.ProjectSlugInput)
	require.JSONEq(t, `{"Plugins":[{
		"ID":"plugin-id",
		"Name":"Kitchen Sink",
		"Slug":"kitchen-sink",
		"Description":"Everything bundle.",
		"IsDefault":false,
		"ServerCount":4,
		"SkillCount":2
	}]}`, out.String())
}

func TestListPluginsToolRequiresService(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := NewListPluginsTool(nil).Call(t.Context(), testToolCallEnv(), bytes.NewBufferString(`{}`), &out)
	require.ErrorContains(t, err, "plugins service not configured")
}

// Not read-only: listing lazily provisions a missing Default plugin (and
// audit logs it) for an org admin, so the descriptor must not promise a pure
// read. It stays non-destructive and idempotent because the heal converges.
func TestListPluginsToolDescriptorReportsTheLazyDefaultPluginWrite(t *testing.T) {
	t.Parallel()

	descriptor := NewListPluginsTool(nil).Descriptor()
	require.Equal(t, "platform_list_plugins", descriptor.Name)
	require.False(t, *descriptor.Annotations.ReadOnlyHint)
	require.False(t, *descriptor.Annotations.DestructiveHint)
	require.True(t, *descriptor.Annotations.IdempotentHint)
	require.False(t, *descriptor.Annotations.OpenWorldHint)
	require.JSONEq(t, `{
		"additionalProperties": false,
		"type": "object"
	}`, string(descriptor.InputSchema))
}
