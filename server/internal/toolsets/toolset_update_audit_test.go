package toolsets_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// toolsetUpdateAuditWant is the toolset a toolset:update entry must name and
// the version it must report.
type toolsetUpdateAuditWant struct {
	toolsetID string
	name      string
	slug      string
	version   int64
}

// requireToolsetUpdateAudit asserts that exactly one toolset:update entry was
// written since before, that it attributes the change to the calling user in
// their organization and project, that it names want's toolset and version, and
// that both snapshots are present with the tool list stripped. It returns the
// decoded snapshots so each caller can assert what its mutation changed.
func requireToolsetUpdateAudit(t *testing.T, ctx context.Context, conn *pgxpool.Pool, before int64, want toolsetUpdateAuditWant) (map[string]any, map[string]any) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "one mutation records exactly one toolset update")

	record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetUpdate), record.Action)
	require.Equal(t, authCtx.ActiveOrganizationID, record.OrganizationID)
	require.Equal(t, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, record.ProjectID)
	require.Equal(t, authCtx.UserID, record.ActorID)
	require.Equal(t, string(urn.PrincipalTypeUser), record.ActorType)
	require.Equal(t, authCtx.Email, record.ActorDisplayName)
	require.Empty(t, record.ActorSlug)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, want.toolsetID, record.SubjectID)
	require.Equal(t, want.name, record.SubjectDisplay)
	require.Equal(t, want.slug, record.SubjectSlug)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"toolset_version_after": float64(want.version)}, metadata)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, want.toolsetID, beforeSnapshot["ID"])
	require.Equal(t, want.toolsetID, afterSnapshot["ID"])
	require.Nil(t, beforeSnapshot["Tools"], "the tool list is stripped from the stored snapshot")
	require.Nil(t, afterSnapshot["Tools"], "the tool list is stripped from the stored snapshot")
	return beforeSnapshot, afterSnapshot
}

func createAuditedToolset(t *testing.T, ctx context.Context, ti *testInstance, name string) *types.Toolset {
	t.Helper()
	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         name,
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	return toolset
}

func TestUpdateToolsetAuditRecordsTheRenamedToolsetAndBothSnapshots(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	created := createAuditedToolset(t, ctx, ti, "Audit Characterisation Original")
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		Slug:        created.Slug,
		Name:        new("Audit Characterisation Renamed"),
		Description: new("after"),
	})
	require.NoError(t, err)

	beforeSnapshot, afterSnapshot := requireToolsetUpdateAudit(t, ctx, ti.conn, before, toolsetUpdateAuditWant{
		toolsetID: created.ID, name: updated.Name, slug: string(updated.Slug), version: updated.ToolsetVersion,
	})
	require.Equal(t, created.Name, beforeSnapshot["Name"], "the before snapshot is read before the write")
	require.Equal(t, updated.Name, afterSnapshot["Name"], "the after snapshot is read after the write")
}

func TestSetToolVariationsGroupAuditRecordsTheGroupChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createAuditedToolset(t, ctx, ti, "Audit Characterisation Variations")
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	updated, err := ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		Slug:                  toolset.Slug,
		ToolVariationsGroupID: &groupID,
	})
	require.NoError(t, err)

	beforeSnapshot, afterSnapshot := requireToolsetUpdateAudit(t, ctx, ti.conn, before, toolsetUpdateAuditWant{
		toolsetID: toolset.ID, name: updated.Name, slug: string(updated.Slug), version: updated.ToolsetVersion,
	})
	require.Nil(t, beforeSnapshot["ToolVariationsGroupID"], "the before snapshot is read before the write")
	require.Equal(t, groupID, afterSnapshot["ToolVariationsGroupID"], "the after snapshot is read after the write")
}

func TestSetUserSessionIssuerAuditRecordsTheIssuerChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createAuditedToolset(t, ctx, ti, "Audit Characterisation Issuer")
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "audit-characterisation",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: (24 * time.Hour).Microseconds(), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	issuerID := issuer.ID.String()
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	updated, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{
		Slug:                toolset.Slug,
		UserSessionIssuerID: &issuerID,
	})
	require.NoError(t, err)

	beforeSnapshot, afterSnapshot := requireToolsetUpdateAudit(t, ctx, ti.conn, before, toolsetUpdateAuditWant{
		toolsetID: toolset.ID, name: updated.Name, slug: string(updated.Slug), version: updated.ToolsetVersion,
	})
	require.Nil(t, beforeSnapshot["UserSessionIssuerID"], "the before snapshot is read before the write")
	require.Equal(t, issuerID, afterSnapshot["UserSessionIssuerID"], "the after snapshot is read after the write")
}

// The tool exposure change runs in a caller-owned transaction, so its audit
// entry is written there too: visible inside it, and gone with a rollback.
func TestChangeToolsetToolsAuditRecordsTheToolChangeInsideTheTransaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createAuditedToolset(t, ctx, ti, "Audit Characterisation Tools")
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	tool, err := urn.ParseTool("tools:function:orders:create_order")
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	change := toolsets.ToolExposureChange{Add: []urn.Tool{tool}}

	rolledBack := testenv.BeginTx(t, ctx, ti.conn)
	_, err = toolsets.ChangeToolsetToolsInTransaction(ctx, rolledBack, testenv.NewLogger(t), audit.NewLogger(), authCtx, toolsetID, toolset.ToolsetVersion, change)
	require.NoError(t, err)
	inside, err := audittest.AuditLogCountByAction(ctx, rolledBack, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, inside, "the entry is written in the caller's transaction")
	require.NoError(t, rolledBack.Rollback(ctx))
	unchanged, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "a rolled-back change leaves no entry")

	tx := testenv.BeginTx(t, ctx, ti.conn)
	result, err := toolsets.ChangeToolsetToolsInTransaction(ctx, tx, testenv.NewLogger(t), audit.NewLogger(), authCtx, toolsetID, toolset.ToolsetVersion, change)
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NoError(t, tx.Commit(ctx))

	beforeSnapshot, afterSnapshot := requireToolsetUpdateAudit(t, ctx, ti.conn, before, toolsetUpdateAuditWant{
		toolsetID: toolset.ID, name: toolset.Name, slug: string(toolset.Slug), version: result.VersionAfter,
	})
	require.Empty(t, beforeSnapshot["ToolUrns"], "the before snapshot is read before the new version")
	require.Equal(t, []any{tool.String()}, afterSnapshot["ToolUrns"], "the after snapshot is read after the new version")

	// A change that applies nothing writes no version and so no entry.
	noop := testenv.BeginTx(t, ctx, ti.conn)
	result, err = toolsets.ChangeToolsetToolsInTransaction(ctx, noop, testenv.NewLogger(t), audit.NewLogger(), authCtx, toolsetID, result.VersionAfter, change)
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.NoError(t, noop.Commit(ctx))
	final, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, final, "a no-op records nothing")
}
