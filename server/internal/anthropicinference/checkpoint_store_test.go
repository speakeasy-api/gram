package anthropicinference

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestPostgresCheckpointConcurrentDeliveries(t *testing.T) {
	t.Parallel()
	testConcurrentCheckpoints(t, false)
}

func TestPostgresCheckpointSingleConnectionPool(t *testing.T) {
	t.Parallel()
	testConcurrentCheckpoints(t, true)
}

func testConcurrentCheckpoints(t *testing.T, singleConnection bool) {
	t.Helper()
	store, db, config := newTestStore(t)
	if singleConnection {
		poolConfig := db.Config()
		poolConfig.MaxConns, poolConfig.MinConns = 1, 0
		small, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
		require.NoError(t, err)
		t.Cleanup(small.Close)
		writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), small, assetstest.NewTestBlobStore(t))
		t.Cleanup(func() { require.NoError(t, shutdown(context.WithoutCancel(t.Context()))) })
		db = small
		store = &postgresStore{db: small, writer: writer}
	}
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "prompt"), textMessage("assistant", "reply"), textMessage("user", "result")}
	firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	var firstCalls, secondCalls atomic.Int32
	scan := func(entered, release chan struct{}, calls *atomic.Int32) scanner {
		return scannerFunc(func(ctx context.Context, _ risk.RealtimeScanRequest) (*risk.ScanResult, error) {
			// Scanners and archival share the same pool. This cannot finish with a
			// one-connection pool if checkpoint coordination pins a connection.
			if _, err := chatrepo.New(db).InferencePolicyRevision(ctx, config.ProjectID); err != nil {
				return nil, fmt.Errorf("scanner database access: %w", err)
			}
			if calls.Add(1) == 1 {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, fmt.Errorf("scanner canceled: %w", ctx.Err())
				}
			}
			return nil, nil
		})
	}
	first := &Service{logger: testenv.NewLogger(t), store: store, scanner: scan(firstEntered, firstRelease, &firstCalls)}
	second := NewService(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, store.writer, scan(secondEntered, secondRelease, &secondCalls))
	firstResult, secondResult := make(chan error, 1), make(chan error, 1)
	firstVerdict, secondVerdict := make(chan Verdict, 1), make(chan Verdict, 1)
	go func() {
		verdict, err := first.Process(t.Context(), config, frame)
		firstVerdict <- verdict
		firstResult <- err
	}()
	select {
	case <-firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery did not reach scanner")
	}
	go func() {
		verdict, err := second.Process(t.Context(), config, frame)
		secondVerdict <- verdict
		secondResult <- err
	}()
	select {
	case <-secondEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("overlapping delivery could not use pool while first scans")
	}
	close(firstRelease)
	select {
	case err := <-firstResult:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery did not complete")
	}
	require.Equal(t, "allow", (<-firstVerdict).Action)
	readMarker := func() []byte {
		marker, err := chatrepo.New(db).GetInferenceAcceptedCheckpoint(t.Context(), chatrepo.GetInferenceAcceptedCheckpointParams{ProjectID: config.ProjectID, ExternalChatID: conv.ToPGText("anthropic-inference:" + conversationID(config, frame).String())})
		require.NoError(t, err)
		return marker
	}
	winner := readMarker()
	close(secondRelease)
	select {
	case err := <-secondResult:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("second delivery did not complete")
	}
	require.Equal(t, "allow", (<-secondVerdict).Action)
	require.Equal(t, winner, readMarker(), "CAS loser must not overwrite winner")
	require.Equal(t, int32(3), firstCalls.Load())
	require.Equal(t, int32(3), secondCalls.Load())
	// Both deliveries allow. A later retry reuses the winning checkpoint
	// and scans only the current turn.
	verdict, err := second.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Equal(t, int32(4), secondCalls.Load())
	session, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	hashes, err := session.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, transcriptHashes(frame.Messages), hashes)
	rows, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, 3)
}

func TestPostgresCheckpointRequiresSuccessfulEvaluation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "prompt"), textMessage("assistant", "blocked reply"), textMessage("user", "benign result")}
	// Rollout: archived messages carry no implicit acceptance.
	saveFrame(t, store, config, frame, "")
	var calls atomic.Int32
	service := NewService(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, store.writer, scannerFunc(func(_ context.Context, r risk.RealtimeScanRequest) (*risk.ScanResult, error) {
		calls.Add(1)
		if r.Text == "blocked reply" {
			return &risk.ScanResult{Action: "block"}, nil
		}
		return nil, nil
	}))
	for range 2 {
		verdict, err := service.Process(t.Context(), config, frame)
		require.NoError(t, err)
		require.Equal(t, "deny", verdict.Action)
		raw, err := chatrepo.New(db).GetInferenceAcceptedCheckpoint(t.Context(), chatrepo.GetInferenceAcceptedCheckpointParams{
			ProjectID: config.ProjectID, ExternalChatID: conv.ToPGText("anthropic-inference:" + conversationID(config, frame).String()),
		})
		require.NoError(t, err)
		require.Empty(t, raw)
	}
	// Denied deliveries leave the checkpoint empty, so each pass scans the
	// transcript again. Inputs scan concurrently and a denial cancels the
	// ones not yet started, so a pass scans between the denied input and
	// every input.
	require.GreaterOrEqual(t, int(calls.Load()), 2*2)
	require.LessOrEqual(t, int(calls.Load()), 2*len(frame.Messages))
	frame.Messages[1] = textMessage("assistant", "safe reply")
	verdict, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	checkpoint, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	hashes, err := checkpoint.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, transcriptHashes(frame.Messages), hashes)
}

func TestPostgresCheckpointInvalidation(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "")
	var expected []byte
	for _, cp := range []acceptedCheckpoint{
		{Version: checkpointVersion + 1, Hashes: transcriptHashes(frame.Messages)},
		{Version: checkpointVersion - 1, Hashes: transcriptHashes(frame.Messages)},
		{Version: checkpointVersion, PolicyRevision: "old-policy-version", Hashes: transcriptHashes(frame.Messages)},
		{Version: checkpointVersion, UserID: "previous-actor", Hashes: transcriptHashes(frame.Messages)},
	} {
		raw, err := json.Marshal(cp)
		require.NoError(t, err)
		_, err = chatrepo.New(db).SetInferenceAcceptedCheckpoint(t.Context(), chatrepo.SetInferenceAcceptedCheckpointParams{
			ProjectID: config.ProjectID, ExternalChatID: conv.ToPGText("anthropic-inference:" + conversationID(config, frame).String()), Checkpoint: raw, ExpectedCheckpoint: expected,
		})
		require.NoError(t, err)
		expected = raw
		session, err := store.Begin(t.Context(), config, frame, "")
		require.NoError(t, err)
		hashes, err := session.Load(t.Context())
		require.NoError(t, err)
		require.Empty(t, hashes)
	}
}

func TestStoreRepeatedFrameBeyondAnchorLimit(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = make([]Message, alignmentAnchors+1)
	for i := range frame.Messages {
		frame.Messages[i] = textMessage("user", "continue")
	}
	saveFrame(t, store, config, frame, "")
	saveFrame(t, store, config, frame, "")
	rows, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, alignmentAnchors+1)
	frame.Messages = append(frame.Messages, textMessage("user", "continue"))
	saveFrame(t, store, config, frame, "")
	saveFrame(t, store, config, frame, "")
	rows, err = chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, alignmentAnchors+1)
}

func TestPostgresCanceledEvaluationPreservesPreviousCheckpoint(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "first prompt"), textMessage("assistant", "first reply")}
	service := NewService(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, store.writer, &recordingScanner{})
	_, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	previous := transcriptHashes(frame.Messages)
	frame.Messages = append(frame.Messages, textMessage("user", "second prompt"), textMessage("assistant", "second reply"), textMessage("user", "result"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	service.scanner = scannerFunc(func(_ context.Context, _ risk.RealtimeScanRequest) (*risk.ScanResult, error) {
		if calls.Add(1) == 2 {
			cancel()
		}
		return nil, nil
	})
	_, err = service.Process(ctx, config, frame)
	require.ErrorIs(t, err, context.Canceled)
	session, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	hashes, err := session.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, previous, hashes)
	rows, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	retry := &recordingScanner{}
	service.scanner = retry
	verdict, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Len(t, retry.inputs, 3)
}

func TestCheckpointCASDetectsIdenticalInterveningAcceptance(t *testing.T) {
	t.Parallel()
	store, _, config := newTestStore(t)
	frame := exampleFrame()
	saveFrame(t, store, config, frame, "")
	hashes := transcriptHashes(frame.Messages)
	initial, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	_, err = initial.Load(t.Context())
	require.NoError(t, err)
	require.NoError(t, initial.Accept(t.Context(), hashes))
	first, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	_, err = first.Load(t.Context())
	require.NoError(t, err)
	second, err := store.Begin(t.Context(), config, frame, "")
	require.NoError(t, err)
	_, err = second.Load(t.Context())
	require.NoError(t, err)
	// Reaccepting identical hashes still changes the token. A stale expected
	// value must conflict instead of silently overwriting this acceptance.
	require.NoError(t, first.Accept(t.Context(), hashes))
	require.ErrorIs(t, second.Accept(t.Context(), hashes), errCheckpointConflict)
}

func TestPostgresLastKnownGoodPreservesDeniedAttempts(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	frame := exampleFrame()
	scanned := &recordingScanner{}
	service := NewService(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, store.writer, scanned)
	verdict, err := service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	readMarker := func() [][]byte {
		session, err := store.Begin(t.Context(), config, frame, "")
		require.NoError(t, err)
		hashes, err := session.Load(t.Context())
		require.NoError(t, err)
		return hashes
	}
	markerA := transcriptHashes(frame.Messages)
	require.Equal(t, markerA, readMarker())
	frame.Messages = append(frame.Messages, textMessage("assistant", "blocked reply"), textMessage("user", "benign result"))
	scanned.result = &risk.ScanResult{Action: "block"}
	for range 2 {
		verdict, err = service.Process(t.Context(), config, frame)
		require.NoError(t, err)
		require.Equal(t, "deny", verdict.Action)
		require.Equal(t, markerA, readMarker())
	}
	frame.Messages[1] = textMessage("assistant", "corrected reply")
	scanned.result = nil
	verdict, err = service.Process(t.Context(), config, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Equal(t, transcriptHashes(frame.Messages), readMarker())
	rows, err := chatrepo.New(db).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, frame), ProjectID: config.ProjectID})
	require.NoError(t, err)
	var content []string
	for _, row := range rows {
		content = append(content, row.Content)
	}
	require.Contains(t, content, "blocked reply")
}
