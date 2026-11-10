package anthropicinference

import (
	"encoding/json"
	"testing"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestServiceOperationIDsStableAcrossRedeliveries(t *testing.T) {
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
	original := scanner.operationIDs
	require.Len(t, original, 2, "only the latest exchange is scanned")
	scanner.reset()
	_, err = service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, original, scanner.operationIDs)
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
	err = testrepo.New(db).ReorderInferenceTranscriptFixture(t.Context(), testrepo.ReorderInferenceTranscriptFixtureParams{ChatID: conversationID(config, frame), ProjectID: conv.ToNullUUID(config.ProjectID)})
	require.NoError(t, err)
	require.Equal(t, len(frame.Messages), saveFrame(t, store, config, frame, userID))
	count, err := chatrepo.New(db).CountInferenceMessages(t.Context(), chatrepo.CountInferenceMessagesParams{ChatID: conversationID(config, frame), ProjectID: conv.ToNullUUID(config.ProjectID)})
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}
