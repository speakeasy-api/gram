package assistants

import (
	"context"
	"net/url"
	"testing"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestKitLocalCompositionSelectsOnlyNewAssistants(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_local_selection")
	require.NoError(t, err)
	project, existing, _, _ := insertAssistantFixture(t, db)
	core := newProvisioningCore(t, db)
	core.guardianPolicy = guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	core.chatWriter = new(chat.ChatMessageWriter)
	core.assistantTokens = assistanttokens.New("test", db, nil, nil, nil)
	core.encryptionClient = new(encryption.Client)
	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	config := KitLocalCompositionConfig{
		Environment: "local", Image: "example.invalid/kit:local", Network: "isolated-test", ExtraCACertFile: "/test/ca.pem", Origin: origin,
		PersistedMCP: true, TurnStream: new(chat.TurnStream),
		ProvisionMCP: func(context.Context, KitMCPProvisionRequest) (KitMCPProvisionResult, error) {
			return KitMCPProvisionResult{}, nil
		},
	}
	require.NoError(t, core.InstallKitLocalRuntime(config))
	created, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Local Kit", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, false, nil)
	require.NoError(t, err)
	require.EqualValues(t, 3, created.RuntimeVersion)
	q := assistantrepo.New(db)
	saved, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: created.ID})
	require.NoError(t, err)
	require.EqualValues(t, 3, saved.RuntimeVersion)
	old, err := q.GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: existing})
	require.NoError(t, err)
	require.EqualValues(t, 2, old.RuntimeVersion)
}

func TestKitLocalBootstrapRejectsUnknownEnvironment(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"", "staging-unknown"} {
		_, _, err := PrepareKitStartupBootstrap(t.Context(), nil, KitStartupBootstrapInput{Environment: environment})
		require.ErrorContains(t, err, "unsupported Kit startup environment")
	}
}
