package triggers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func newCreatePayload(envID uuid.UUID, name string) *gen.CreateTriggerInstancePayload {
	envIDStr := envID.String()
	return &gen.CreateTriggerInstancePayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DefinitionSlug:   "slack",
		Name:             name,
		EnvironmentID:    &envIDStr,
		TargetKind:       bgtriggers.TargetKindNoop,
		TargetRef:        "noop-ref",
		TargetDisplay:    "Noop",
		Config:           map[string]any{},
		Status:           nil,
	}
}

func TestCreateTriggerInstance_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "audit-create"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	row, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTriggerInstanceCreate)
	require.NoError(t, err)
	require.Equal(t, "trigger_instance", row.SubjectType)
	require.Equal(t, "audit-create", row.SubjectDisplay)
	require.Equal(t, "slack", row.SubjectSlug)
}

func TestDeleteTriggerInstance_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "audit-delete"))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceDelete)
	require.NoError(t, err)

	err = ti.service.DeleteTriggerInstance(ctx, &gen.DeleteTriggerInstancePayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	row, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTriggerInstanceDelete)
	require.NoError(t, err)
	require.Equal(t, "trigger_instance", row.SubjectType)
	require.Equal(t, "audit-delete", row.SubjectDisplay)
	require.Equal(t, "slack", row.SubjectSlug)
}

func TestPauseTriggerInstance_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateTriggerInstance(ctx, newCreatePayload(ti.environmentID, "audit-pause"))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstancePause)
	require.NoError(t, err)

	paused, err := ti.service.PauseTriggerInstance(ctx, &gen.PauseTriggerInstancePayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, bgtriggers.StatusPaused, paused.Status)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstancePause)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	row, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTriggerInstancePause)
	require.NoError(t, err)
	require.Equal(t, "trigger_instance", row.SubjectType)
	require.Equal(t, "audit-pause", row.SubjectDisplay)
	require.Equal(t, "slack", row.SubjectSlug)
}

func TestResumeTriggerInstance_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateTriggerInstance(ctx, &gen.CreateTriggerInstancePayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DefinitionSlug:   "slack",
		Name:             "audit-resume",
		EnvironmentID:    conv.PtrEmpty(ti.environmentID.String()),
		TargetKind:       bgtriggers.TargetKindNoop,
		TargetRef:        "noop-ref",
		TargetDisplay:    "Noop",
		Config:           map[string]any{},
		Status:           conv.PtrEmpty(bgtriggers.StatusPaused),
	})
	require.NoError(t, err)
	require.Equal(t, bgtriggers.StatusPaused, created.Status)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceResume)
	require.NoError(t, err)

	resumed, err := ti.service.ResumeTriggerInstance(ctx, &gen.ResumeTriggerInstancePayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, bgtriggers.StatusActive, resumed.Status)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTriggerInstanceResume)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	row, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTriggerInstanceResume)
	require.NoError(t, err)
	require.Equal(t, "trigger_instance", row.SubjectType)
	require.Equal(t, "audit-resume", row.SubjectDisplay)
	require.Equal(t, "slack", row.SubjectSlug)
}
