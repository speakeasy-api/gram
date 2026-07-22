package skills

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
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

	// A repeat load of the same version writes the same activation again, and a
	// second wake is safe: wakes carry no payload and coalesce onto one pass.
	out.Reset()
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(chatID.String()), bytes.NewBufferString(`{"name":"waking-skill"}`), &out))
	require.Equal(t, []uuid.UUID{fixture.projectID, fixture.projectID}, signaler.signaled())
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
