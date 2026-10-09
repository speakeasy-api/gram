package environments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
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
	slug := "other-" + uuid.NewString()[:8]
	other, err := projectsrepo.New(f.ti.conn).CreateProject(f.ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: f.orgID})
	require.NoError(t, err)
	otherEnv, err := repo.New(f.ti.conn).CreateEnvironment(f.ctx, repo.CreateEnvironmentParams{
		OrganizationID: f.orgID, ProjectID: other.ID, Name: slug, Slug: slug, Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	unlinked := f.toolset(t, other.ID)
	requireCode(t, f.setToolset(f.ctx, unlinked, f.readable), oops.CodeNotFound)

	linked := f.toolset(t, other.ID)
	_, err = repo.New(f.ti.conn).SetToolsetEnvironment(f.ctx, repo.SetToolsetEnvironmentParams{
		ToolsetID: uuid.MustParse(linked), ProjectID: other.ID, EnvironmentID: otherEnv.ID,
	})
	require.NoError(t, err)
	requireCode(t, f.setToolset(f.ctx, linked, f.readable), oops.CodeNotFound)

	stored, err := repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: uuid.MustParse(linked), ProjectID: other.ID,
	})
	require.NoError(t, err)
	require.Equal(t, otherEnv.ID, stored)
	_, err = repo.New(f.ti.conn).LockToolsetEnvironmentBinding(f.ctx, repo.LockToolsetEnvironmentBindingParams{
		ToolsetID: uuid.MustParse(unlinked), ProjectID: other.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
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
