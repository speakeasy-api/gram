package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newShadowTranscripts keeps encrypted shadows for one project and returns
// the store, so a test can capture them.
func newShadowTranscripts(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) (*transcriptstore.Store, *transcriptstore.Transcripts) {
	t.Helper()
	keys := orgencryptiontest.New(t, conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(ctx, organizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(conn, keys.Service, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	return store, transcriptstore.NewTranscripts(store, projectID)
}

func captureShadows(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *transcriptstore.Store, organizationID string, projectID uuid.UUID, ids ...uuid.UUID) {
	t.Helper()
	require.NoError(t, store.Prepare(ctx, organizationID), "writers load the key before the transaction")
	tx, err := conn.Begin(ctx) //nolint:glint // notestingrawsql: Capture runs inside the caller's message transaction; message rows come from generated seed queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, store.Capture(ctx, tx, organizationID, projectID, ids))
	require.NoError(t, tx.Commit(ctx))
}

// editBehindShadow changes a message's plaintext without recapturing its
// shadow, so the shadow is stale.
func editBehindShadow(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, messageID uuid.UUID) {
	t.Helper()
	changed, err := fixturerepo.New(conn).ChangeContent(ctx, messageID, uuid.NullUUID{UUID: projectID, Valid: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)
}

func TestChatSearchAndReadServePlaintextForStaleShadows(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
	ctx := f.context(t.Context())
	store, transcripts := newShadowTranscripts(t, ctx, f.db, f.principal.OrganizationID, f.project.ID)
	f.service.WithTranscripts(transcripts)

	chatID := f.chat(t, ctx, "user_fixture_owner", "shadowed")
	messageID := seedRecallMessage(t, ctx, f.db, chatID, f.project.ID, recallMessageSeed{role: "user", content: "rotate the staging certificate", at: f.base})
	captureShadows(t, ctx, f.db, store, f.principal.OrganizationID, f.project.ID, messageID)

	output, err := f.service.Search(ctx, f.principal, f.search("staging certificate"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 1)
	require.Contains(t, output.Hits[0].Text, "rotate the staging certificate")
	read, err := f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: messageID.String()})
	require.NoError(t, err)
	require.Len(t, read.Messages, 1)
	require.Contains(t, read.Messages[0].Text, "rotate the staging certificate")

	// The chunked search matches the transcript, never the plaintext edited
	// behind it. A hit whose transcript disagrees with its plaintext row is
	// logged and served from plaintext, which stays the source of truth, and
	// the read tool serves it too.
	editBehindShadow(t, ctx, f.db, f.project.ID, messageID)
	output, err = f.service.Search(ctx, f.principal, f.search("changed"))
	require.NoError(t, err)
	require.Empty(t, output.Hits, "the edited plaintext is not searched")
	output, err = f.service.Search(ctx, f.principal, f.search("staging certificate"))
	require.NoError(t, err)
	require.Len(t, output.Hits, 1)
	require.Contains(t, output.Hits[0].Text, "changed")
	read, err = f.service.Read(ctx, f.principal, ReadChatInput{ProjectID: f.project.ID.String(), ChatID: chatID.String(), MessageID: messageID.String()})
	require.NoError(t, err)
	require.Len(t, read.Messages, 1)
	require.Contains(t, read.Messages[0].Text, "changed")
}

func TestSessionRecallServesPlaintextForStaleShadows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_shadow")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, transcripts := newShadowTranscripts(t, ctx, conn, principal.OrganizationID, project.ID)
	enabled := true
	svc, _ := newRecallService(t, conn, &enabled)
	svc.WithTranscripts(transcripts)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "shadowed session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	messageID := seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: "rotate the staging certificate", at: time.Now().UTC().Add(-time.Hour), generation: 0, source: "claude-code", analyzed: true,
	})
	captureShadows(t, ctx, conn, store, principal.OrganizationID, project.ID, messageID)

	out, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	require.Contains(t, out.Digest, "rotate the staging certificate")

	// A disagreeing shadow is logged and the plaintext row is served.
	editBehindShadow(t, ctx, conn, project.ID, messageID)
	out, err = svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	require.Contains(t, out.Digest, "changed")
}
