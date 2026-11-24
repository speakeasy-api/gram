package gram

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBackfillTranscriptShadowsSealsEveryPageAndResumes(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "transcript_shadow_backfill")
	require.NoError(t, err)

	org, projectID, chatID := "org_"+uuid.NewString(), uuid.New(), uuid.New()
	fixtures := fixturerepo.New(db)
	_, err = fixtures.CreateOrganization(t.Context(), org)
	require.NoError(t, err)
	_, err = fixtures.CreateProject(t.Context(), projectID, org)
	require.NoError(t, err)
	_, err = fixtures.CreateChat(t.Context(), chatID, projectID, org)
	require.NoError(t, err)
	const messages = 5
	for range messages {
		_, err = fixtures.CreateMessage(t.Context(), uuid.New(), chatID, uuid.NullUUID{UUID: projectID, Valid: true})
		require.NoError(t, err)
	}

	keys := orgencryptiontest.New(t, db)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(db, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	// One page of two, then resume from its cursor until the project is covered.
	sealed, cursor, err := backfillTranscriptShadows(t.Context(), store, org, projectID, uuid.Nil, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 2, sealed)
	require.NotEqual(t, uuid.Nil, cursor)

	sealed, _, err = backfillTranscriptShadows(t.Context(), store, org, projectID, cursor, 2, 0)
	require.NoError(t, err)
	require.Equal(t, messages-2, sealed)

	progress, err := store.Progress(t.Context(), org, projectID)
	require.NoError(t, err)
	require.Equal(t, transcriptstore.Progress{Current: messages, Waiting: 0, Unattributed: 0}, progress)
}
