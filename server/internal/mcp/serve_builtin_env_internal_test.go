package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type builtinEnvironmentFixture struct {
	load func(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error)
}

func (f builtinEnvironmentFixture) Load(ctx context.Context, projectID uuid.UUID, selected toolconfig.SlugOrID) (map[string]string, error) {
	return f.load(ctx, projectID, selected)
}
func (f builtinEnvironmentFixture) LoadSystemEnv(context.Context, uuid.UUID, uuid.UUID, string, string) (*toolconfig.CaseInsensitiveEnv, error) {
	return nil, errors.New("unexpected system environment load")
}

func TestBuiltinDefaultEnvironmentResolution(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	selectedID := uuid.New()
	definition, _ := builtinmcp.Lookup("slack")
	for _, tc := range []struct {
		name          string
		settings      builtinmcp.Settings
		request       string
		headers       map[string]string
		loadErr       error
		expected      toolconfig.SlugOrID
		allowed       bool
		expectedToken string
	}{
		{name: "project standard default", expected: toolconfig.Slug("default"), allowed: true, expectedToken: "stored"},
		{name: "configured default override", settings: builtinmcp.Settings{EnvironmentID: uuid.NullUUID{UUID: selectedID, Valid: true}}, expected: toolconfig.ID(selectedID), allowed: true, expectedToken: "stored"},
		{name: "direct override", request: "other", expected: toolconfig.Slug("other"), allowed: true, expectedToken: "stored"},
		{name: "missing default fails closed", loadErr: toolconfig.ErrNotFound, expected: toolconfig.Slug("default")},
		{name: "header-only legacy project", loadErr: toolconfig.ErrNotFound, headers: map[string]string{"slack_bot_token": "header"}, expected: toolconfig.Slug("default"), allowed: true, expectedToken: "header"},
		{name: "unrelated header is not a credential", loadErr: toolconfig.ErrNotFound, headers: map[string]string{"OTHER_TOKEN": "header"}, expected: toolconfig.Slug("default")},
		{name: "explicit missing environment does not fall back", request: "missing", loadErr: toolconfig.ErrNotFound, headers: map[string]string{"SLACK_BOT_TOKEN": "header"}, expected: toolconfig.Slug("missing")},
		{name: "store failure does not fall back", loadErr: errors.New("storage unavailable"), headers: map[string]string{"SLACK_BOT_TOKEN": "header"}, expected: toolconfig.Slug("default")},
		{name: "header overrides stored token", headers: map[string]string{"SLACK_BOT_TOKEN": "header"}, expected: toolconfig.Slug("default"), allowed: true, expectedToken: "header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loader := builtinEnvironmentFixture{load: func(_ context.Context, actualProject uuid.UUID, selected toolconfig.SlugOrID) (map[string]string, error) {
				require.Equal(t, projectID, actualProject)
				require.Equal(t, tc.expected, selected)
				return map[string]string{"SLACK_BOT_TOKEN": "stored", "UNDECLARED_SECRET": "never-forward"}, tc.loadErr
			}}
			env, err := resolveBuiltinEnvironment(t.Context(), loader, projectID, definition, tc.settings, uuid.NullUUID{}, tc.request, tc.headers)
			if !tc.allowed {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedToken, env.UserConfig.Get("SLACK_BOT_TOKEN"))
			require.Empty(t, env.UserConfig.Get("UNDECLARED_SECRET"))
		})
	}
}
