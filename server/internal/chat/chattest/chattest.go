// Package chattest builds the chat write path over test infrastructure.
package chattest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// NewTurnStream returns a turn stream over a Redis client from env.
func NewTurnStream(t *testing.T, env *testenv.Environment) *chat.TurnStream {
	t.Helper()
	redisClient, err := env.NewRedisClient(t, 0)
	require.NoError(t, err)
	return chat.NewTurnStream(redisClient)
}

// NewMessageWriter returns a chat message writer over db with test asset
// storage. The writer is shut down when the test finishes.
func NewMessageWriter(t *testing.T, env *testenv.Environment, db *pgxpool.Pool) *chat.ChatMessageWriter {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), db, assetstest.NewTestBlobStore(t), NewTurnStream(t, env))
	t.Cleanup(func() { require.NoError(t, shutdown(context.WithoutCancel(t.Context()))) })
	return writer
}
