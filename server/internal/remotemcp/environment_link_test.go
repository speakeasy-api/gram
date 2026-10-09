package remotemcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	remotemcpserver "github.com/speakeasy-api/gram/server/gen/http/remote_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type remoteLinkFixture struct {
	ctx       context.Context //nolint:containedctx // the fixture's authenticated request context, shared by every call it makes
	ti        *testInstance
	projectID uuid.UUID
	remoteID  uuid.UUID
	envID     uuid.UUID
}

func newRemoteLinkFixture(t *testing.T) remoteLinkFixture {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "env-" + uuid.NewString()[:8]
	env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return remoteLinkFixture{
		ctx:       ctx,
		ti:        ti,
		projectID: *authCtx.ProjectID,
		remoteID:  uuid.MustParse(createTestServer(t, ctx, ti).ID),
		envID:     env.ID,
	}
}

func (f remoteLinkFixture) wrapper(t *testing.T, remoteID uuid.UUID, linked bool, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	id := uuid.New()
	server, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(f.ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                id,
		ProjectID:         f.projectID,
		Name:              conv.ToPGText("wrapper " + id.String()[:8]),
		Slug:              conv.ToPGText("wrapper-" + id.String()[:8]),
		EnvironmentID:     uuid.NullUUID{UUID: f.envID, Valid: linked},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
		Visibility:        visibility,
		NetworkAccessMode: conv.ToPGText("public_only"),
	})
	require.NoError(t, err)
	return server
}

func (f remoteLinkFixture) mcpWriteOnly(t *testing.T) context.Context {
	t.Helper()
	return withExactAccessGrants(t, f.ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, f.projectID.String()))
}

func (f remoteLinkFixture) withEnvironmentAuthority(t *testing.T) context.Context {
	t.Helper()
	return withExactAccessGrants(t, f.ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, f.projectID.String()), authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
		authz.SelectorKeyResourceKind: "environment",
		authz.SelectorKeyResourceID:   authz.WildcardResource,
		authz.SelectorKeyProjectID:    f.projectID.String(),
	}))
}

func (f remoteLinkFixture) storedURL(t *testing.T) string {
	t.Helper()

	row, err := repo.New(f.ti.conn).GetServerByID(f.ctx, repo.GetServerByIDParams{ID: f.remoteID, ProjectID: f.projectID})
	require.NoError(t, err)
	return row.Url
}

func urlUpdate(id uuid.UUID, url string) *gen.UpdateServerPayload {
	return &gen.UpdateServerPayload{ID: id.String(), URL: &url}
}

func remoteUpdateAudits(t *testing.T, ctx context.Context, conn *pgxpool.Pool) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionRemoteMcpServerUpdate)
	require.NoError(t, err)
	return count
}

func TestUpdateServer_URLChangeWithEnvironmentLinkedServerRequiresEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T, f remoteLinkFixture){
		"linked private server": func(t *testing.T, f remoteLinkFixture) {
			t.Helper()
			f.wrapper(t, f.remoteID, true, "private")
		},
		"linked disabled server": func(t *testing.T, f remoteLinkFixture) {
			t.Helper()
			f.wrapper(t, f.remoteID, true, "disabled")
		},
		"one of two servers linked": func(t *testing.T, f remoteLinkFixture) {
			t.Helper()
			f.wrapper(t, f.remoteID, false, "private")
			f.wrapper(t, f.remoteID, true, "private")
		},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newRemoteLinkFixture(t)
			seed(t, f)
			originalURL := f.storedURL(t)
			beforeAudits := remoteUpdateAudits(t, f.ctx, f.ti.conn)

			_, err := f.ti.service.UpdateServer(f.mcpWriteOnly(t), urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Equal(t, originalURL, f.storedURL(t))
			require.Equal(t, beforeAudits, remoteUpdateAudits(t, f.ctx, f.ti.conn))

			updated, err := f.ti.service.UpdateServer(f.withEnvironmentAuthority(t), urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
			require.NoError(t, err)
			require.Equal(t, "https://moved.example.com/mcp", updated.URL)
		})
	}
}

func TestUpdateServer_WithoutURLChangeNeedsNoEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	f.wrapper(t, f.remoteID, true, "private")
	writeOnly := f.mcpWriteOnly(t)

	// Same URL and a name-only edit both leave the destination alone.
	_, err := f.ti.service.UpdateServer(writeOnly, urlUpdate(f.remoteID, f.storedURL(t)))
	require.NoError(t, err)
	name := "renamed"
	_, err = f.ti.service.UpdateServer(writeOnly, &gen.UpdateServerPayload{ID: f.remoteID.String(), Name: &name})
	require.NoError(t, err)
}

func TestUpdateServer_URLChangeWithUnlinkedOrDeletedServersNeedsNoEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	f.wrapper(t, f.remoteID, false, "private")
	deleted := f.wrapper(t, f.remoteID, true, "private")
	_, err := mcpserversrepo.New(f.ti.conn).DeleteMCPServer(f.ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	// A linked server on another remote source does not count.
	f.wrapper(t, uuid.MustParse(createTestServer(t, f.ctx, f.ti).ID), true, "private")

	updated, err := f.ti.service.UpdateServer(f.mcpWriteOnly(t), urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, "https://moved.example.com/mcp", updated.URL)
}

// A URL change that waits behind an uncommitted link must see that link once
// it commits.
func TestUpdateServer_URLChangeWaitsForConcurrentEnvironmentLink(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	wrapper := f.wrapper(t, f.remoteID, false, "private")
	originalURL := f.storedURL(t)

	// The linking writer, as UpdateMcpServer runs it: project lock, then row.
	tx, err := f.ti.conn.Begin(f.ctx) //nolint:glint // notestingrawsql: a held transaction stands in for a concurrent environment link
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(f.ctx)) })
	require.NoError(t, admission.LockProject(f.ctx, tx, f.projectID))
	_, err = mcpserversrepo.New(tx).UpdateMCPServer(f.ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:              wrapper.Name,
		Slug:              wrapper.Slug,
		EnvironmentID:     uuid.NullUUID{UUID: f.envID, Valid: true},
		RemoteMcpServerID: wrapper.RemoteMcpServerID,
		Visibility:        wrapper.Visibility,
		ID:                wrapper.ID,
		ProjectID:         f.projectID,
	})
	require.NoError(t, err)

	writeOnly := f.mcpWriteOnly(t)
	done := make(chan error, 1)
	go func() {
		_, err := f.ti.service.UpdateServer(writeOnly, urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
		done <- err
	}()

	testenv.WaitForQueryBlockedBy(t, f.ctx, f.ti.conn, testenv.BackendPID(tx), "%LockProjectEnforcementState :exec%")
	require.Empty(t, done, "URL change must wait for the project lock")
	require.NoError(t, tx.Commit(f.ctx))

	select {
	case err := <-done:
		requireOopsCode(t, err, oops.CodeForbidden)
	case <-f.ctx.Done():
		t.Fatal("URL change did not finish after the link committed")
	}
	require.Equal(t, originalURL, f.storedURL(t))
}

func getServer(t *testing.T, ctx context.Context, f remoteLinkFixture, id uuid.UUID) *types.RemoteMcpServer {
	t.Helper()

	idStr := id.String()
	server, err := f.ti.service.GetServer(ctx, &gen.GetServerPayload{ID: &idStr})
	require.NoError(t, err)
	return server
}

// getServer reports a linked server even when the caller cannot list it, so
// the dashboard never unlocks a destination change from a filtered inventory.
func TestGetServer_EnvironmentLinkedCountsServersTheCallerCannotList(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	f.wrapper(t, f.remoteID, false, "private")
	hidden := f.wrapper(t, f.remoteID, true, "disabled")

	// Write across the project, but reading the linked server is blocked.
	caller := withExactAccessGrants(t, f.ctx, f.ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   hidden.ID.String(),
		}),
	)

	require.True(t, conv.PtrValOr(getServer(t, caller, f, f.remoteID).EnvironmentLinked, false))
	_, err := f.ti.service.UpdateServer(caller, urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetServer_EnvironmentLinkedIsFalseForUnlinkedSources(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	f.wrapper(t, f.remoteID, false, "private")
	deleted := f.wrapper(t, f.remoteID, true, "private")
	_, err := mcpserversrepo.New(f.ti.conn).DeleteMCPServer(f.ctx, mcpserversrepo.DeleteMCPServerParams{ID: deleted.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	// A linked server on another source does not count.
	f.wrapper(t, uuid.MustParse(createTestServer(t, f.ctx, f.ti).ID), true, "private")

	linked := getServer(t, f.ctx, f, f.remoteID).EnvironmentLinked
	require.NotNil(t, linked)
	require.False(t, *linked)
}

func (f remoteLinkFixture) secondEnvironment(t *testing.T) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	slug := "env-" + uuid.NewString()[:8]
	env, err := environmentsrepo.New(f.ti.conn).CreateEnvironment(f.ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      f.projectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return env.ID
}

// linkedTo seeds a server on the remote linked to environmentID.
func (f remoteLinkFixture) linkedTo(t *testing.T, environmentID uuid.UUID, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	id := uuid.New()
	server, err := mcpserversrepo.New(f.ti.conn).CreateMCPServer(f.ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                id,
		ProjectID:         f.projectID,
		Name:              conv.ToPGText("wrapper " + id.String()[:8]),
		Slug:              conv.ToPGText("wrapper-" + id.String()[:8]),
		EnvironmentID:     uuid.NullUUID{UUID: environmentID, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: f.remoteID, Valid: true},
		Visibility:        visibility,
		NetworkAccessMode: conv.ToPGText("public_only"),
	})
	require.NoError(t, err)
	return server
}

func (f remoteLinkFixture) excludedFrom(t *testing.T, environmentID uuid.UUID, extra ...authz.Grant) context.Context {
	t.Helper()
	grants := append([]authz.Grant{
		authz.NewGrant(authz.ScopeMCPWrite, f.projectID.String()),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
			authz.SelectorKeyResourceKind: "environment",
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    f.projectID.String(),
		}),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentBlockedRead, authz.Selector{
			authz.SelectorKeyResourceKind: "environment",
			authz.SelectorKeyResourceID:   environmentID.String(),
			authz.SelectorKeyProjectID:    f.projectID.String(),
		}),
	}, extra...)
	return withExactAccessGrants(t, f.ctx, f.ti.conn, grants...)
}

// Every environment linked on the source counts, including one reached only
// through a disabled server the caller cannot list.
func TestUpdateServer_URLChangeRefusedWhenAnyLinkedEnvironmentIsExcluded(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	readable := f.secondEnvironment(t)
	f.linkedTo(t, readable, "private")
	hidden := f.linkedTo(t, f.envID, "disabled")
	originalURL := f.storedURL(t)

	caller := f.excludedFrom(t, f.envID, authz.NewGrantWithSelector(authz.ScopeMCPBlockedRead, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   hidden.ID.String(),
	}))
	got := getServer(t, caller, f, f.remoteID)
	require.True(t, conv.PtrValOr(got.EnvironmentLinked, false))
	require.False(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, true))

	_, err := f.ti.service.UpdateServer(caller, urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, originalURL, f.storedURL(t))

	// Excluded from an environment the source does not use: allowed.
	other := f.excludedFrom(t, f.secondEnvironment(t))
	got = getServer(t, other, f, f.remoteID)
	require.True(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, false))
	_, err = f.ti.service.UpdateServer(other, urlUpdate(f.remoteID, "https://moved.example.com/mcp"))
	require.NoError(t, err)
}

func TestGetServer_EnvironmentLinkAuthorizedNamesNoEnvironment(t *testing.T) {
	t.Parallel()

	f := newRemoteLinkFixture(t)
	f.linkedTo(t, f.envID, "private")

	got := getServer(t, f.mcpWriteOnly(t), f, f.remoteID)
	require.True(t, conv.PtrValOr(got.EnvironmentLinked, false))
	require.False(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, true))
	// Encode the response as the API sends it: no environment identifier.
	body, err := json.Marshal(remotemcpserver.NewGetServerResponseBody(got))
	require.NoError(t, err)
	require.Contains(t, string(body), `"environment_link_authorized":false`)
	require.NotContains(t, string(body), f.envID.String())

	got = getServer(t, f.withEnvironmentAuthority(t), f, f.remoteID)
	require.True(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, false))

	// Unlinked source: nothing to authorize.
	unlinked := uuid.MustParse(createTestServer(t, f.ctx, f.ti).ID)
	got = getServer(t, f.mcpWriteOnly(t), f, unlinked)
	require.False(t, conv.PtrValOr(got.EnvironmentLinked, true))
	require.True(t, conv.PtrValOr(got.EnvironmentLinkAuthorized, false))
}
