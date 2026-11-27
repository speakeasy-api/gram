package reader // want `remove ".*reader.Gone" from the plaintext transcript reader allowlist in glint: the function no longer exists`

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/plaintextread/repo"
)

type Service struct {
	q *repo.Queries
}

// Sanctioned is on the sanctioned allowlist in the test.
func (s *Service) Sanctioned(ctx context.Context) {
	_, _ = s.q.ListMessages(ctx, "chat")
}

// Pending is on the pending allowlist in the test.
func Pending(ctx context.Context, q *repo.Queries) {
	run := func() {
		_, _ = q.CountToolCalls(ctx)
	}
	run()
}

func (s *Service) Stale(ctx context.Context) { // want `remove "\(\*github.com/speakeasy-api/gram/server/internal/plaintextread/reader.Service\).Stale" from the plaintext transcript reader allowlist in glint: it no longer reads plaintext transcript columns`
	_, _ = s.q.ListMessageIDs(ctx, "chat")
}

func Leaky(ctx context.Context, q *repo.Queries) {
	_, _ = q.ListMessages(ctx, "chat") // want `do not call "ListMessages" here: it reads the plaintext transcript columns of chat_messages; move the query`
	_, _ = q.CountToolCalls(ctx)       // want `do not call "CountToolCalls" here`
	_, _ = q.FindByNotes(ctx)          // want `do not call "FindByNotes" here`
	list := q.ListMessages             // want `do not call "ListMessages" here`
	_ = q.SetNotes(ctx, "id")          // want `do not call "SetNotes" here: it updates the plaintext transcript columns of chat_messages; change the body through transcriptstore.Transcripts.UpdateBody`
	_ = q.UpsertMessage(ctx, "id")     // want `do not call "UpsertMessage" here: it updates`
	_ = q.CreateMessage(ctx, "chat")   // want `do not call "CreateMessage" here: it inserts the plaintext transcript columns of chat_messages; insert messages through the chat message writer`
	_ = list
}

func Clean(ctx context.Context, q *repo.Queries) {
	_, _ = q.ListMessageIDs(ctx, "chat")
	_, _ = q.ListPartBodies(ctx, "chat")
	_, _ = q.KeyInJSON(ctx)
}

const rawBodyUpdate = "UPDATE chat_messages SET content = $1 WHERE id = $2" // want `this SQL updates the plaintext transcript columns of chat_messages; change the body through transcriptstore.Transcripts.UpdateBody`

const rawMarkUpdate = "UPDATE chat_messages SET risk_analyzed_at = now() WHERE id = $1"
