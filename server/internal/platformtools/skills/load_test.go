package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
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

func TestSkillsLoadReturnsTurnSelectedSkillWithoutDistribution(t *testing.T) {
	t.Parallel()

	ctx, fixture := newSkillLoadFixture(t, "turn-selected-skill")
	_, err := skillsrepo.New(fixture.conn).RevokeActiveSkillDistribution(ctx, skillsrepo.RevokeActiveSkillDistributionParams{
		ProjectID:   fixture.projectID,
		SkillID:     fixture.version.SkillID,
		PluginID:    uuid.NullUUID{},
		AssistantID: uuid.NullUUID{UUID: fixture.assistantID, Valid: true},
		Channel:     "assistant",
	})
	require.NoError(t, err)

	payload, err := json.Marshal(map[string]any{
		"text": "use the selected skill",
		"skill_set_snapshot": map[string]any{
			"version": 1,
			"skills": []map[string]any{{
				"skill_id":            fixture.version.SkillID,
				"name":                "turn-selected-skill",
				"description":         "first",
				"resolved_version_id": fixture.version.ID,
			}},
		},
	})
	require.NoError(t, err)
	_, err = assistantrepo.New(fixture.conn).InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     fixture.threadID,
		AssistantID:           fixture.assistantID,
		ProjectID:             fixture.projectID,
		TriggerInstanceID:     uuid.NullUUID{},
		EventID:               uuid.NewString(),
		CorrelationID:         uuid.NewString(),
		Status:                "processing",
		NormalizedPayloadJson: payload,
		SourcePayloadJson:     payload,
	})
	require.NoError(t, err)

	var out bytes.Buffer
	tool := NewLoadTool(testenv.NewLogger(t), fixture.conn)
	require.NoError(t, tool.Call(ctx, skillToolCallEnv(fixture.chatID.String()), bytes.NewBufferString(`{"name":"turn-selected-skill"}`), &out))
	require.Equal(t, fixture.version.Content, out.String())
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
