package anthropicinference

import (
	"encoding/json"
	"errors"
	"testing"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestServiceArchivesBeforeCheckpointLoadFailure(t *testing.T) {
	t.Parallel()
	loadErr := errors.New("checkpoint unavailable")
	store := &memoryStore{loadErr: loadErr}
	scanner := &recordingScanner{}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	_, err := service.Process(t.Context(), Config{}, exampleFrame())
	require.ErrorIs(t, err, loadErr)
	require.Len(t, store.saved, 1)
	require.Empty(t, scanner.inputs)
	require.Empty(t, store.accepted)
}

func TestServiceOperationIDsStableAcrossAcceptedMultiBlockHistory(t *testing.T) {
	t.Parallel()
	store := &memoryStore{}
	scanner := &recordingScanner{}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"first"},{"type":"text","text":"second"}]`)},
		textMessage("assistant", "reply"),
		textMessage("user", "current"),
	}
	_, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Len(t, scanner.operationIDs, 4)
	original := scanner.operationIDs[3]
	scanner.reset()
	_, err = service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, []string{original}, scanner.operationIDs)
}

func TestStoreAlignsEqualTimestampMessagesBySequence(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = append(frame.Messages, textMessage("assistant", "reply"), textMessage("user", "next"))
	userID, err := store.ResolveActor(t.Context(), config, frame)
	require.NoError(t, err)
	saveFrame(t, store, config, frame, userID)
	// Assign tied timestamps and UUIDs in reverse transcript order, so UUID
	// ordering cannot accidentally produce the expected alignment.
	//nolint:glint // notestingrawsql: Deliberately rewrite immutable row IDs and timestamps to exercise ordering; not an application query.
	_, err = db.Exec(t.Context(), `UPDATE chat_messages SET created_at = '2026-01-01', id = CASE content WHEN 'EXAMPLE prompt' THEN 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid WHEN 'reply' THEN 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee'::uuid ELSE 'dddddddd-dddd-dddd-dddd-dddddddddddd'::uuid END WHERE chat_id = $1 AND project_id = $2`, conversationID(config, frame), config.ProjectID)
	require.NoError(t, err)
	require.Equal(t, len(frame.Messages), saveFrame(t, store, config, frame, userID))
	count, err := chatrepo.New(db).CountInferenceMessages(t.Context(), chatrepo.CountInferenceMessagesParams{ChatID: conversationID(config, frame), ProjectID: conv.ToNullUUID(config.ProjectID)})
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}
