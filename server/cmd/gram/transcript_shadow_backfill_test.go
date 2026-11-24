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
	ids := make([]uuid.UUID, 0, messages)
	for range messages {
		id := uuid.New()
		_, err = fixtures.CreateMessage(t.Context(), id, chatID, uuid.NullUUID{UUID: projectID, Valid: true})
		require.NoError(t, err)
		ids = append(ids, id)
	}

	keys := orgencryptiontest.New(t, db)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(db, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	// One page of two, then resume from its cursor until the project is covered.
	run, err := backfillTranscriptShadows(t.Context(), store, org, projectID, uuid.Nil, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 2, run.Sealed)
	require.False(t, run.Complete)
	require.NotEqual(t, uuid.Nil, run.After)

	run, err = backfillTranscriptShadows(t.Context(), store, org, projectID, run.After, 2, 0)
	require.NoError(t, err)
	require.Equal(t, messages-2, run.Sealed)
	require.True(t, run.Complete)

	// Coverage counts fingerprints; opening every shadow proves they decrypt.
	opened, err := store.LoadMany(t.Context(), org, projectID, ids)
	require.NoError(t, err)
	require.Len(t, opened, messages)

	progress, err := store.Progress(t.Context(), org, projectID)
	require.NoError(t, err)
	require.Equal(t, transcriptstore.Progress{Current: messages, Waiting: 0, Unattributed: 0}, progress)
}
