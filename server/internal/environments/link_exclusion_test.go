package environments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// Linking needs read access to every environment the change affects, not only
// a project-wide grant: an exclusion naming one environment refuses linking
// it, replacing it, or removing it.

type linkFixture struct {
	ctx       context.Context //nolint:containedctx // the fixture's authenticated request context, shared by every call it makes
	ti        *testInstance
	orgID     string
	projectID uuid.UUID
	excluded  string
	readable  string
}

func newLinkExclusionFixture(t *testing.T) linkFixture {
	t.Helper()

	ctx, ti := newTestEnvironmentService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := linkFixture{ctx: ctx, ti: ti, orgID: authCtx.ActiveOrganizationID, projectID: *authCtx.ProjectID}
	f.excluded = f.environment(t, "excluded")
	f.readable = f.environment(t, "readable")
	return f
}

func (f linkFixture) environment(t *testing.T, name string) string {
	t.Helper()

	env, err := f.ti.service.CreateEnvironment(f.ctx, &gen.CreateEnvironmentPayload{
		Name:    name + "-" + uuid.NewString()[:8],
		Entries: []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)
	return env.ID
}

// caller holds project-wide environment:read but is blocked from excluded.
func (f linkFixture) caller(t *testing.T) context.Context {
	t.Helper()
	return authztest.WithExactGrants(t, f.ctx,
		envGrant(authz.ScopeEnvironmentRead, f.projectID.String()),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentBlockedRead, authz.Selector{
			authz.SelectorKeyResourceKind: "environment",
			authz.SelectorKeyResourceID:   f.excluded,
			authz.SelectorKeyProjectID:    f.projectID.String(),
		}),
	)
}

func (f linkFixture) toolset(t *testing.T, projectID uuid.UUID) string {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.ti.conn).CreateToolset(f.ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         f.orgID,
		ProjectID:              projectID,
		Name:                   "linked toolset",
		Slug:                   "linked-" + uuid.NewString()[:8],
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	return toolset.ID.String()
}

// otherProject creates a second project in the fixture's organization with
// one environment, returning both ids.
func (f linkFixture) otherProject(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()

	slug := "other-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(f.ti.conn).CreateProject(f.ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: f.orgID})
	require.NoError(t, err)
	env, err := repo.New(f.ti.conn).CreateEnvironment(f.ctx, repo.CreateEnvironmentParams{
		OrganizationID: f.orgID, ProjectID: project.ID, Name: slug, Slug: slug, Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return project.ID, env.ID
}

func requireCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func (f linkFixture) sourceBinding(t *testing.T, slug string) string {
	t.Helper()

	id, err := repo.New(f.ti.conn).LockSourceEnvironmentBinding(f.ctx, repo.LockSourceEnvironmentBindingParams{
		SourceKind: "http", SourceSlug: slug, ProjectID: f.projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	require.NoError(t, err)
	return id.String()
}

func (f linkFixture) setSource(ctx context.Context, slug, environmentID string) error {
	_, err := f.ti.service.SetSourceEnvironmentLink(ctx, &gen.SetSourceEnvironmentLinkPayload{
		SourceKind: gen.SourceKind("http"), SourceSlug: slug, EnvironmentID: environmentID,
	})
	return err //nolint:wrapcheck // returned for oops-code assertions
}

func (f linkFixture) setToolset(ctx context.Context, toolsetID, environmentID string) error {
	_, err := f.ti.service.SetToolsetEnvironmentLink(ctx, &gen.SetToolsetEnvironmentLinkPayload{
		ToolsetID: toolsetID, EnvironmentID: environmentID,
	})
	return err //nolint:wrapcheck // returned for oops-code assertions
}

func TestSourceEnvironmentLink_ExclusionRefusesEveryAffectedLinkChange(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	caller := f.caller(t)

	// Linking the excluded environment.
	requireCode(t, f.setSource(caller, "a", f.excluded), oops.CodeForbidden)
	require.Empty(t, f.sourceBinding(t, "a"))

	// Replacing an excluded link with a readable one unlinks the excluded one.
	require.NoError(t, f.setSource(f.ctx, "b", f.excluded))
	requireCode(t, f.setSource(caller, "b", f.readable), oops.CodeForbidden)
	require.Equal(t, f.excluded, f.sourceBinding(t, "b"))

	// Removing an excluded link.
	err := f.ti.service.DeleteSourceEnvironmentLink(caller, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "b"})
	requireCode(t, err, oops.CodeForbidden)
	require.Equal(t, f.excluded, f.sourceBinding(t, "b"))

	// Readable environments are unaffected.
	require.NoError(t, f.setSource(caller, "c", f.readable))
	require.NoError(t, f.ti.service.DeleteSourceEnvironmentLink(caller, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "c"}))
	require.Empty(t, f.sourceBinding(t, "c"))

	// Deleting a link that does not exist stays a no-op.
	require.NoError(t, f.ti.service.DeleteSourceEnvironmentLink(caller, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "missing"}))
}

func TestToolsetEnvironmentLink_ExclusionRefusesEveryAffectedLinkChange(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	caller := f.caller(t)
	binding := func(toolsetID string) string {
		id, err := repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
			ToolsetID: uuid.MustParse(toolsetID), ProjectID: f.projectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ""
		}
		require.NoError(t, err)
		return id.String()
	}

	a := f.toolset(t, f.projectID)
	requireCode(t, f.setToolset(caller, a, f.excluded), oops.CodeForbidden)
	require.Empty(t, binding(a))

	b := f.toolset(t, f.projectID)
	require.NoError(t, f.setToolset(f.ctx, b, f.excluded))
	requireCode(t, f.setToolset(caller, b, f.readable), oops.CodeForbidden)
	err := f.ti.service.DeleteToolsetEnvironmentLink(caller, &gen.DeleteToolsetEnvironmentLinkPayload{ToolsetID: b})
	requireCode(t, err, oops.CodeForbidden)
	require.Equal(t, f.excluded, binding(b))

	c := f.toolset(t, f.projectID)
	require.NoError(t, f.setToolset(caller, c, f.readable))
	require.NoError(t, f.ti.service.DeleteToolsetEnvironmentLink(caller, &gen.DeleteToolsetEnvironmentLinkPayload{ToolsetID: c}))
	require.Empty(t, binding(c))
	require.NoError(t, f.ti.service.DeleteToolsetEnvironmentLink(caller, &gen.DeleteToolsetEnvironmentLinkPayload{ToolsetID: c}))
}

// A toolset in another project can be neither linked nor have its existing
// link rewritten from this project.
func TestToolsetEnvironmentLink_RefusesAnotherProjectsToolset(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	otherProjectID, otherEnvID := f.otherProject(t)

	unlinked := f.toolset(t, otherProjectID)
	requireCode(t, f.setToolset(f.ctx, unlinked, f.readable), oops.CodeNotFound)

	linked := f.toolset(t, otherProjectID)
	_, err := repo.New(f.ti.conn).SetToolsetEnvironment(f.ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID: uuid.MustParse(linked), ProjectID: otherProjectID, EnvironmentID: otherEnvID,
	})
	require.NoError(t, err)
	requireCode(t, f.setToolset(f.ctx, linked, f.readable), oops.CodeNotFound)

	stored, err := repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: uuid.MustParse(linked), ProjectID: otherProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, otherEnvID, stored)
	_, err = repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: uuid.MustParse(unlinked), ProjectID: otherProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// The write itself refuses an environment from another project, so a caller
// that skipped the handler's environment check still cannot bind one.
func TestSetToolsetEnvironment_RefusesAnotherProjectsEnvironment(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	_, otherEnvID := f.otherProject(t)

	unlinked := uuid.MustParse(f.toolset(t, f.projectID))
	_, err := repo.New(f.ti.conn).SetToolsetEnvironment(f.ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID: unlinked, ProjectID: f.projectID, EnvironmentID: otherEnvID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: unlinked, ProjectID: f.projectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	linked := uuid.MustParse(f.toolset(t, f.projectID))
	_, err = repo.New(f.ti.conn).SetToolsetEnvironment(f.ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID: linked, ProjectID: f.projectID, EnvironmentID: uuid.MustParse(f.readable),
	})
	require.NoError(t, err)
	_, err = repo.New(f.ti.conn).SetToolsetEnvironment(f.ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID: linked, ProjectID: f.projectID, EnvironmentID: otherEnvID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	stored, err := repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: linked, ProjectID: f.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(f.readable), stored)
}

// The write itself refuses an environment from another project, so a caller
// that skipped the handler's environment check still cannot bind one.
func TestSetSourceEnvironment_RefusesAnotherProjectsEnvironment(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	_, otherEnvID := f.otherProject(t)
	set := func(slug string, environmentID uuid.UUID) error {
		_, err := repo.New(f.ti.conn).SetSourceEnvironment(f.ctx, repo.SetSourceEnvironmentParams{
			SourceKind: "http", SourceSlug: slug, ProjectID: f.projectID, EnvironmentID: environmentID,
		})
		return err //nolint:wrapcheck // returned for pgx.ErrNoRows assertions
	}

	require.ErrorIs(t, set("unlinked", otherEnvID), pgx.ErrNoRows)
	require.Empty(t, f.sourceBinding(t, "unlinked"))

	require.NoError(t, set("linked", uuid.MustParse(f.readable)))
	require.ErrorIs(t, set("linked", otherEnvID), pgx.ErrNoRows)
	require.Equal(t, f.readable, f.sourceBinding(t, "linked"))
}

// Link changes take the project lock MCP-server links and source destination
// changes take, so their binding read cannot go stale before the write.
func TestSourceEnvironmentLink_TakesProjectLock(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	tx, err := f.ti.conn.Begin(f.ctx) //nolint:glint // notestingrawsql: a held transaction stands in for a concurrent holder of the project lock
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(f.ctx)) })
	require.NoError(t, admission.LockProject(f.ctx, tx, f.projectID))

	done := make(chan error, 1)
	go func() { done <- f.setSource(f.ctx, "locked", f.readable) }()

	testenv.WaitForQueryBlockedBy(t, f.ctx, f.ti.conn, testenv.BackendPID(tx), "%LockProjectEnforcementState :exec%")
	require.Empty(t, done, "link change must wait for the project lock")
	require.NoError(t, tx.Commit(f.ctx))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-f.ctx.Done():
		t.Fatal("link change did not finish after the lock was released")
	}
	require.Equal(t, f.readable, f.sourceBinding(t, "locked"))
}

func auditCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, action audit.Action) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, conn, action)
	require.NoError(t, err)
	return count
}

func requireBindingAudit(t *testing.T, f linkFixture, action audit.Action, environmentID string, metadata map[string]any) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	record, err := audittest.LatestAuditLogByAction(f.ctx, f.ti.conn, action)
	require.NoError(t, err)
	require.Equal(t, "environment", record.SubjectType)
	require.Equal(t, environmentID, record.SubjectID)
	require.Equal(t, authCtx.UserID, record.ActorID)
	require.Equal(t, uuid.NullUUID{UUID: f.projectID, Valid: true}, record.ProjectID)
	got, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, metadata, got)
}

func TestSourceEnvironmentLink_AuditsEachBindingChange(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	links := func() int64 { return auditCount(t, f.ctx, f.ti.conn, audit.ActionEnvironmentSourceLink) }
	unlinks := func() int64 { return auditCount(t, f.ctx, f.ti.conn, audit.ActionEnvironmentSourceUnlink) }
	target := map[string]any{"source_kind": "http", "source_slug": "audited"}

	require.NoError(t, f.setSource(f.ctx, "audited", f.readable))
	require.Equal(t, int64(1), links())
	require.Equal(t, int64(0), unlinks())
	requireBindingAudit(t, f, audit.ActionEnvironmentSourceLink, f.readable, target)

	// Setting the same environment again changes nothing.
	require.NoError(t, f.setSource(f.ctx, "audited", f.readable))
	require.Equal(t, int64(1), links())

	// A replacement unlinks the old environment and links the new one.
	require.NoError(t, f.setSource(f.ctx, "audited", f.excluded))
	require.Equal(t, int64(2), links())
	require.Equal(t, int64(1), unlinks())
	requireBindingAudit(t, f, audit.ActionEnvironmentSourceUnlink, f.readable, target)
	requireBindingAudit(t, f, audit.ActionEnvironmentSourceLink, f.excluded, target)

	require.NoError(t, f.ti.service.DeleteSourceEnvironmentLink(f.ctx, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "audited"}))
	require.Equal(t, int64(2), unlinks())
	requireBindingAudit(t, f, audit.ActionEnvironmentSourceUnlink, f.excluded, target)

	// Deleting a link that does not exist records nothing.
	require.NoError(t, f.ti.service.DeleteSourceEnvironmentLink(f.ctx, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "audited"}))
	require.Equal(t, int64(2), unlinks())
}

func TestToolsetEnvironmentLink_AuditsEachBindingChange(t *testing.T) {
	t.Parallel()

	f := newLinkExclusionFixture(t)
	toolsetID := f.toolset(t, f.projectID)
	target := map[string]any{"toolset_id": toolsetID}

	require.NoError(t, f.setToolset(f.ctx, toolsetID, f.readable))
	requireBindingAudit(t, f, audit.ActionEnvironmentToolsetLink, f.readable, target)

	require.NoError(t, f.setToolset(f.ctx, toolsetID, f.excluded))
	requireBindingAudit(t, f, audit.ActionEnvironmentToolsetUnlink, f.readable, target)
	requireBindingAudit(t, f, audit.ActionEnvironmentToolsetLink, f.excluded, target)

	require.NoError(t, f.ti.service.DeleteToolsetEnvironmentLink(f.ctx, &gen.DeleteToolsetEnvironmentLinkPayload{ToolsetID: toolsetID}))
	requireBindingAudit(t, f, audit.ActionEnvironmentToolsetUnlink, f.excluded, target)
	require.Equal(t, int64(2), auditCount(t, f.ctx, f.ti.conn, audit.ActionEnvironmentToolsetLink))
	require.Equal(t, int64(2), auditCount(t, f.ctx, f.ti.conn, audit.ActionEnvironmentToolsetUnlink))
}

// The binding and its audit entry commit together.
func TestEnvironmentLink_AuditFailureRollsBackBinding(t *testing.T) {
	t.Parallel()

	t.Run("source", func(t *testing.T) {
		t.Parallel()

		f := newLinkExclusionFixture(t)
		require.NoError(t, f.setSource(f.ctx, "rollback", f.readable))
		require.NoError(t, audittest.RejectAction(f.ctx, f.ti.conn, audit.ActionEnvironmentSourceUnlink))

		require.Error(t, f.setSource(f.ctx, "rollback", f.excluded))
		require.Equal(t, f.readable, f.sourceBinding(t, "rollback"))
		require.Error(t, f.ti.service.DeleteSourceEnvironmentLink(f.ctx, &gen.DeleteSourceEnvironmentLinkPayload{SourceKind: gen.SourceKind("http"), SourceSlug: "rollback"}))
		require.Equal(t, f.readable, f.sourceBinding(t, "rollback"))
	})

	t.Run("toolset", func(t *testing.T) {
		t.Parallel()

		f := newLinkExclusionFixture(t)
		toolsetID := f.toolset(t, f.projectID)
		require.NoError(t, audittest.RejectAction(f.ctx, f.ti.conn, audit.ActionEnvironmentToolsetLink))

		require.Error(t, f.setToolset(f.ctx, toolsetID, f.readable))
		_, err := repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
			ToolsetID: uuid.MustParse(toolsetID), ProjectID: f.projectID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})
}
