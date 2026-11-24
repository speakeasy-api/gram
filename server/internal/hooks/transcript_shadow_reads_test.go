package hooks

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// shadowLoad is one LoadMany call a hook request made.
type shadowLoad struct {
	// requested are the message IDs the call asked to open.
	requested []uuid.UUID

	// opened are the message IDs that had a shadow and opened.
	opened []uuid.UUID

	// err is what the store returned.
	err error
}

// recordingShadowReader wraps the real store and records every LoadMany call,
// so a test can prove which reads went through shadows and how many.
type recordingShadowReader struct {
	store *transcriptstore.Store
	mu    sync.Mutex
	loads []shadowLoad
}

func (r *recordingShadowReader) LoadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.Message, error) {
	opened, err := r.store.LoadMany(ctx, organizationID, projectID, messageIDs)
	load := shadowLoad{requested: slices.Clone(messageIDs), opened: make([]uuid.UUID, 0, len(opened)), err: err}
	for id := range opened {
		load.opened = append(load.opened, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads = append(r.loads, load)
	return opened, err //nolint:wrapcheck // the wrapper must return the store's error unchanged
}

func (r *recordingShadowReader) recorded() []shadowLoad {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.loads)
}

type shadowReadFixture struct {
	ti        *testInstance
	orgID     string
	projectID uuid.UUID
	store     *transcriptstore.Store
	sessionID string
	chatID    uuid.UUID
}

func newShadowReadFixture(t *testing.T) (context.Context, shadowReadFixture) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), authCtx.ActiveOrganizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(ti.conn, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)

	sessionID := "shadow-read-" + uuid.NewString()
	return ctx, shadowReadFixture{
		ti:        ti,
		orgID:     authCtx.ActiveOrganizationID,
		projectID: *authCtx.ProjectID,
		store:     store,
		sessionID: sessionID,
		chatID:    sessionIDToUUID(sessionID),
	}
}

// readThrough routes this service's hook reconciliation for the given project
// through a recording wrapper around the fixture's store.
func (f shadowReadFixture) readThrough(projectID uuid.UUID) *recordingShadowReader {
	f.ti.service.SetTranscriptShadowReads(f.store, projectID)
	reader := &recordingShadowReader{store: f.store, mu: sync.Mutex{}, loads: nil}
	f.ti.service.transcriptShadow = reader
	return reader
}

func (f shadowReadFixture) capture() {
	f.ti.service.writer.WithTranscriptShadow(f.store, f.projectID)
}

func (f shadowReadFixture) submitPrompt(ctx context.Context, t *testing.T, text string) uuid.UUID {
	t.Helper()
	payload := canonicalIngestPayload("claude", "prompt.submitted", f.sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
	res, err := f.ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(f.ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: f.chatID, ProjectID: f.projectID})
	require.NoError(t, err)
	for _, m := range msgs {
		if m.Role == "user" && m.Content == text {
			return m.ID
		}
	}
	require.FailNow(t, "prompt row not stored")
	return uuid.Nil
}

// attachToPrompt ships one attachment that names its prompt by the hash of
// promptText, the path that reads stored prompt bodies.
func (f shadowReadFixture) attachToPrompt(ctx context.Context, t *testing.T, promptText string) string {
	t.Helper()
	entryUUID := "attachment-" + uuid.NewString()
	promptSHA256 := testPromptSHA256(promptText)
	payload := canonicalIngestPayload("claude", "assistant.responded", f.sessionID)
	payload.Data = &gen.HookIngestData{
		PromptAttachments: []*gen.HookPromptAttachmentEntry{{
			EntryUUID:      entryUUID,
			PromptSha256:   &promptSHA256,
			AttachmentKind: "file",
			Content:        "invented demo attachment body",
		}},
	}
	res, err := f.ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)
	return entryUUID
}

// attachmentParents maps each stored attachment's external ID to its parent
// prompt.
func (f shadowReadFixture) attachmentParents(ctx context.Context, t *testing.T, parents ...uuid.UUID) map[string]uuid.UUID {
	t.Helper()
	parts, err := chatRepo.New(f.ti.conn).ListChatContentPartsByChatID(ctx, chatRepo.ListChatContentPartsByChatIDParams{
		ChatID:               f.chatID,
		ProjectID:            f.projectID,
		ParentChatMessageIds: parents,
	})
	require.NoError(t, err)
	got := make(map[string]uuid.UUID, len(parts))
	for _, part := range parts {
		got[part.ExternalID.String] = part.ParentChatMessageID.UUID
	}
	return got
}

func TestIngest_ShadowReadProjectMatchesPromptAttachmentsThroughShadows(t *testing.T) {
	t.Parallel()
	ctx, f := newShadowReadFixture(t)
	f.capture()
	reader := f.readThrough(f.projectID)

	first := f.submitPrompt(ctx, t, "Plan the invented garden layout")
	second := f.submitPrompt(ctx, t, "List invented seed varieties")
	entry := f.attachToPrompt(ctx, t, "Plan the invented garden layout")

	require.Equal(t, map[string]uuid.UUID{entry: first}, f.attachmentParents(ctx, t, first, second))
	loads := reader.recorded()
	require.Len(t, loads, 1, "one hook request opens its candidates with one LoadMany call")
	require.NoError(t, loads[0].err)
	require.ElementsMatch(t, []uuid.UUID{first, second}, loads[0].requested)
	require.ElementsMatch(t, []uuid.UUID{first, second}, loads[0].opened)
}

func TestIngest_ShadowReadProjectSkipsShadowsWhenNoAttachmentNamesItsPrompt(t *testing.T) {
	t.Parallel()
	ctx, f := newShadowReadFixture(t)
	f.capture()
	reader := f.readThrough(f.projectID)

	latest := f.submitPrompt(ctx, t, "Describe the invented harbor")
	entryUUID := "attachment-" + uuid.NewString()
	payload := canonicalIngestPayload("claude", "assistant.responded", f.sessionID)
	payload.Data = &gen.HookIngestData{
		PromptAttachments: []*gen.HookPromptAttachmentEntry{{
			EntryUUID:      entryUUID,
			AttachmentKind: "file",
			Content:        "invented demo attachment body",
		}},
	}
	res, err := f.ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	require.Equal(t, map[string]uuid.UUID{entryUUID: latest}, f.attachmentParents(ctx, t, latest))
	require.Empty(t, reader.recorded(), "the latest prompt is chosen without reading any body")
}

func TestIngest_ShadowReadProjectMatchesPlaintextWhenNoShadowExists(t *testing.T) {
	t.Parallel()
	ctx, f := newShadowReadFixture(t)
	// Written before capture began, so it has no shadow.
	uncaptured := f.submitPrompt(ctx, t, "Sketch the invented lighthouse")
	f.capture()
	reader := f.readThrough(f.projectID)
	captured := f.submitPrompt(ctx, t, "Name the invented island")

	entry := f.attachToPrompt(ctx, t, "Sketch the invented lighthouse")

	require.Equal(t, map[string]uuid.UUID{entry: uncaptured}, f.attachmentParents(ctx, t, uncaptured, captured))
	loads := reader.recorded()
	require.Len(t, loads, 1)
	require.NoError(t, loads[0].err)
	require.ElementsMatch(t, []uuid.UUID{uncaptured, captured}, loads[0].requested)
	require.Equal(t, []uuid.UUID{captured}, loads[0].opened)
}

func TestIngest_ShadowReadProjectFailsOnAStaleShadow(t *testing.T) {
	t.Parallel()
	ctx, f := newShadowReadFixture(t)
	f.capture()
	reader := f.readThrough(f.projectID)
	edited := f.submitPrompt(ctx, t, "Draft the invented menu")

	// A write that skips capture leaves the shadow behind the plaintext.
	changed, err := fixturerepo.New(f.ti.conn).ChangeContent(t.Context(), edited, uuid.NullUUID{UUID: f.projectID, Valid: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)

	// The hash names the edited plaintext, so a plaintext fallback would
	// attach here. The stale shadow must fail the match instead.
	f.attachToPrompt(ctx, t, "changed")

	require.Empty(t, f.attachmentParents(ctx, t, edited))
	loads := reader.recorded()
	require.Len(t, loads, 1)
	require.ErrorIs(t, loads[0].err, transcriptstore.ErrStale)
}

func TestIngest_OtherProjectsMatchPromptAttachmentsFromPlaintext(t *testing.T) {
	t.Parallel()
	ctx, f := newShadowReadFixture(t)
	f.capture()
	// Only another project reads through shadows.
	reader := f.readThrough(uuid.New())

	prompt := f.submitPrompt(ctx, t, "Outline the invented festival")
	entry := f.attachToPrompt(ctx, t, "Outline the invented festival")

	require.Equal(t, map[string]uuid.UUID{entry: prompt}, f.attachmentParents(ctx, t, prompt))
	require.Empty(t, reader.recorded(), "a project off the read allowlist never opens shadows")
}
