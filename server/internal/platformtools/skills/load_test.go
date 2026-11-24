package skills

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSkillsLoadWakesEfficacyAfterRecordedActivation(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "waking-skill")
	signaler := &recordingEfficacySignaler{}
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn, WithEfficacySignaler(signaler))
	chatID := uuid.New()

	var out bytes.Buffer
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"waking-skill"}`), &out))
	require.Equal(t, fixture.version.Content, out.String())
	require.Equal(t, []uuid.UUID{fixture.projectID}, signaler.signaled())

	observations, err := hooksrepo.New(fixture.conn).ListSkillObservations(ctx, fixture.projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1)

	// A repeat load refreshes recency and may harmlessly wake efficacy again.
	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"waking-skill"}`), &out))
	require.Equal(t, []uuid.UUID{fixture.projectID, fixture.projectID}, signaler.signaled())
}

func TestSkillsLoadListsAndReadsSupportingFiles(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "directory-skill")
	queries := skillsrepo.New(fixture.conn)
	reference := "# Forms\nFill in every field.\n"
	logo := []byte{0x89, 'P', 'N', 'G', 0x00}
	for _, file := range []struct {
		path, contentType string
		content           []byte
	}{
		{path: "references/FORMS.md", contentType: "text/markdown; charset=utf-8", content: []byte(reference)},
		{path: "assets/logo.png", contentType: "image/png", content: logo},
	} {
		require.NoError(t, queries.CreateSkillVersionFile(ctx, skillsrepo.CreateSkillVersionFileParams{
			Path: file.path, Content: file.content, Sha256: "deadbeef", SizeBytes: int64(len(file.content)),
			ContentType: file.contentType, ProjectID: fixture.projectID, SkillID: fixture.version.SkillID, SkillVersionID: fixture.version.ID,
		}))
	}
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	chatID := uuid.NewString()

	var out bytes.Buffer
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"directory-skill"}`), &out))
	require.True(t, strings.HasPrefix(out.String(), fixture.version.Content), "SKILL.md comes first, unchanged")
	require.Contains(t, out.String(), "- assets/logo.png (image/png, 5 bytes)")
	require.Contains(t, out.String(), "- references/FORMS.md (text/markdown; charset=utf-8, ")

	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"directory-skill","path":"references/FORMS.md"}`), &out))
	require.Equal(t, reference, out.String())

	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"directory-skill","path":"assets/logo.png"}`), &out))
	require.Contains(t, out.String(), "assets/logo.png is a binary file (image/png, 5 bytes")
	require.NotContains(t, out.String(), string(logo))

	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"directory-skill","path":"SKILL.md"}`), &out))
	require.True(t, strings.HasPrefix(out.String(), fixture.version.Content))

	err := tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"directory-skill","path":"references/MISSING.md"}`), &out)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)

	observations, err := hooksrepo.New(fixture.conn).ListSkillObservations(ctx, fixture.projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1, "only loading SKILL.md is an activation; reading a supporting file is not")
}

func TestSkillsLoadWithoutSupportingFilesIsExactlySkillMD(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "single-file-skill")
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn)

	var out bytes.Buffer
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"name":"single-file-skill"}`), &out))
	require.Equal(t, fixture.version.Content, out.String())
}

func TestSkillsLoadSignalFailureDoesNotAlterResult(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "refused-wake-skill")
	signaler := &recordingEfficacySignaler{err: errors.New("coordinator unreachable")}
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn, WithEfficacySignaler(signaler))

	var out bytes.Buffer
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"name":"refused-wake-skill"}`), &out))
	require.Equal(t, fixture.version.Content, out.String())
	require.Len(t, signaler.signaled(), 1)

	observations, err := hooksrepo.New(fixture.conn).ListSkillObservations(ctx, fixture.projectID)
	require.NoError(t, err)
	require.Len(t, observations, 1)
}

func TestSkillsLoadSkipsWakeWithoutRecordableChatID(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "unattributed-skill")
	signaler := &recordingEfficacySignaler{}
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn, WithEfficacySignaler(signaler))

	for _, chatID := range []string{"", "not-a-uuid", uuid.Nil.String()} {
		var out bytes.Buffer
		require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID), bytes.NewBufferString(`{"name":"unattributed-skill"}`), &out))
		require.Equal(t, fixture.version.Content, out.String())
	}

	require.Empty(t, signaler.signaled(), "an activation that was never recorded wakes nothing")
}

// closePoolWriter kills the pool the activation write needs, from inside the
// write of the content the caller receives.
type closePoolWriter struct {
	content []byte
	pool    *pgxpool.Pool
}

func (w *closePoolWriter) Write(p []byte) (int, error) {
	w.content = append(w.content, p...)
	w.pool.Close()
	return len(p), nil
}

func TestSkillsLoadSkipsWakeWhenActivationRecordFails(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "unrecorded-skill")
	signaler := &recordingEfficacySignaler{}
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn, WithEfficacySignaler(signaler))
	writer := &closePoolWriter{content: nil, pool: fixture.conn}

	require.NoError(t, tool.Call(ctx, skillToolCallEnv(uuid.NewString()), bytes.NewBufferString(`{"name":"unrecorded-skill"}`), writer))
	require.Equal(t, fixture.version.Content, string(writer.content))
	require.Empty(t, signaler.signaled(), "a failed activation write wakes nothing")
}
