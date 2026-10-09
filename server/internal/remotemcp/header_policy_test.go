package remotemcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func projectID(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}

// seedLegacyHeader stores a header exactly as given, bypassing write
// validation, the way rows written before names were canonicalized look.
func seedLegacyHeader(t *testing.T, ctx context.Context, ti *testInstance, serverID string, name string, value string, source string) repo.RemoteMcpServerHeader {
	t.Helper()

	return remotemcptest.SeedHeader(t, ctx, ti.conn, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      uuid.MustParse(serverID),
		ProjectID:              projectID(t, ctx),
		Name:                   name,
		Description:            conv.PtrToPGText(nil),
		IsRequired:             true,
		IsSecret:               false,
		Value:                  conv.PtrToPGTextEmpty(&value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(&source),
	})
}

func TestCreateServerHeader_RejectsProtectedPassThroughSources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	for _, source := range []string{"Authorization", "authorization", "Gram-Key", "gRaM-cHaT-sEsSiOn", "Gram_Session", "Gram-Project", "Gram-Consent-State", "X-Gram-Tunnel-Id", "X-Gram-Agent-Version", "X_Speakeasy_Identity", "Proxy-Authorization", "Set-Cookie"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
			p.ValueFromRequestHeader = new(source)
		}))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Authorization")
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Contains(t, oopsErr.Error(), "separate request header", "the refusal explains the remediation")
}

func TestCreateServerHeader_RejectsReservedDestinations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	for _, name := range []string{"Set-Cookie", "set-cookie", "Proxy-Authorization", "Mcp-Method", "Mcp_Method", "MCP-Protocol-Version"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, name, func(p *gen.CreateServerHeaderPayload) {
			p.Value = new("v")
		}))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "Cookie", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Upstream-Cookie")
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_AllowsOperatorCredentials(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	for _, name := range []string{"Cookie", "Gram-Key", "Authorization"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, name, func(p *gen.CreateServerHeaderPayload) {
			p.IsSecret = new(true)
			p.Value = new("operator-credential")
		}))
		require.NoError(t, err, name)
	}

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Authorization", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Service-Token")
	}))
	require.NoError(t, err)
	require.Equal(t, "X-Service-Token", *header.ValueFromRequestHeader)
}

func TestCreateServerHeader_RejectsInvalidNamesAndValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	for _, name := range []string{"X Bad", "X-Bad\r\nX-Injected", "X-Bad:", "X-Bad\t"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, name, func(p *gen.CreateServerHeaderPayload) {
			p.Value = new("v")
		}))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X Bad")
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)

	for _, value := range []string{"line1\r\nX-Injected: 1", "line1\nline2", "nul\x00"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
			p.IsSecret = new(true)
			p.Value = new(value)
		}))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Tabbed", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("a\tb")
	}))
	require.NoError(t, err, "horizontal tab is a valid field value byte")
	require.Equal(t, "a\tb", *header.Value)
}

func TestCreateServerHeader_StoresCanonicalNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, " x-forwarded-token ", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("x-caller-token")
	}))
	require.NoError(t, err)
	require.Equal(t, "X-Forwarded-Token", header.Name)
	require.Equal(t, "X-Caller-Token", *header.ValueFromRequestHeader)
}

func TestCreateServerHeader_CaseInsensitiveDuplicateConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	seedLegacyHeader(t, ctx, ti, server.ID, "x-api-key", "legacy", "")

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-KEY", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("new")
	}))
	requireOopsCode(t, err, oops.CodeConflict)

	// The same name on another server is unrelated.
	other := createTestServer(t, ctx, ti)
	_, err = ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(other.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("new")
	}))
	require.NoError(t, err)
}

func TestCreateServerHeader_DeletedRowDoesNotConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	legacy := seedLegacyHeader(t, ctx, ti, server.ID, "x-api-key", "legacy", "")
	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               legacy.ID.String(),
	}))

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("new")
	}))
	require.NoError(t, err)
}

func TestUpdateServerHeader_CaseInsensitiveDuplicateConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	seedLegacyHeader(t, ctx, ti, server.ID, "x-api-key", "legacy", "")
	other := createSecretHeader(t, ctx, ti, server.ID, "X-Other", "other-secret")

	// A value-preserving secret update gets no exception.
	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(other.ID, "X-Api-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
	}))
	requireOopsCode(t, err, oops.CodeConflict)
	requireStoredSecretValue(t, ctx, ti, server.ID, "X-Other", "other-secret")
}

func TestUpdateServerHeader_CanonicalizesLegacyRowWithoutConflictingWithItself(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	legacy := seedLegacyHeader(t, ctx, ti, server.ID, "x-api-key", "legacy", "")

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(legacy.ID.String(), "x-api-key", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("rotated")
	}))
	require.NoError(t, err)
	require.Equal(t, "X-Api-Key", updated.Name)
}

// Editing a secret stored under a noncanonical name without resupplying its
// value validates and canonicalizes the name and keeps the ciphertext.
func TestUpdateServerHeader_LegacySecretKeepsValueWhenRenamed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	legacy, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      uuid.MustParse(server.ID),
		ProjectID:              projectID(t, ctx),
		Name:                   "x-api-key",
		Description:            conv.PtrToPGText(nil),
		IsRequired:             true,
		IsSecret:               true,
		Value:                  conv.ToPGText("legacy-secret"),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(legacy.ID.String(), "x-api-key", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.IsRequired = new(true)
	}))
	require.NoError(t, err)
	require.Equal(t, "X-Api-Key", updated.Name)
	requireStoredSecretValue(t, ctx, ti, server.ID, "X-Api-Key", "legacy-secret")

	// The name is still validated when the value is preserved.
	_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(legacy.ID.String(), "Set-Cookie", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)
	requireStoredSecretValue(t, ctx, ti, server.ID, "X-Api-Key", "legacy-secret")
}

func TestUpdateServerHeader_RejectsProtectedPassThroughSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	legacy := seedLegacyHeader(t, ctx, ti, server.ID, "X-Upstream-Token", "", "Authorization")

	// Keeping the protected source while changing anything else is refused.
	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(legacy.ID.String(), "X-Upstream-Token", func(p *gen.UpdateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Authorization")
	}))
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Repairing it to a separately supplied header works.
	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(legacy.ID.String(), "X-Upstream-Token", func(p *gen.UpdateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Client-Upstream-Token")
	}))
	require.NoError(t, err)
	require.Equal(t, "X-Client-Upstream-Token", *updated.ValueFromRequestHeader)
}

// A header write locks the parent server and checks for a case-insensitive
// duplicate only after it holds the lock, so a duplicate committed by a writer
// that held the lock first is seen.
func TestCreateServerHeader_DuplicateCheckWaitsForServerLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	project := projectID(t, ctx)

	holder, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return holder.Rollback(context.Background()) })

	_, err = repo.New(holder).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: uuid.MustParse(server.ID), ProjectID: project})
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "x-api-key", func(p *gen.CreateServerHeaderPayload) {
			p.Value = new("contender")
		}))
		result <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(holder), "%FROM remote_mcp_servers%FOR UPDATE%")

	value := "holder"
	_, err = repo.New(holder).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      uuid.MustParse(server.ID),
		ProjectID:              project,
		Name:                   "X-Api-Key",
		Description:            conv.PtrToPGText(nil),
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.PtrToPGTextEmpty(&value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))

	requireOopsCode(t, <-result, oops.CodeConflict)
}

// Server deletion locks the server before its headers, the same order header
// writers use, so a header created while the deletion waits is deleted with
// it and the two never deadlock.
func TestDeleteServer_LocksServerBeforeHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	project := projectID(t, ctx)

	holder, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return holder.Rollback(context.Background()) })

	_, err = repo.New(holder).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: uuid.MustParse(server.ID), ProjectID: project})
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		result <- ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			ID:               server.ID,
		})
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(holder), "%FROM remote_mcp_servers%FOR UPDATE%")

	value := "created-while-deleting"
	_, err = repo.New(holder).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      uuid.MustParse(server.ID),
		ProjectID:              project,
		Name:                   "X-Late",
		Description:            conv.PtrToPGText(nil),
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.PtrToPGTextEmpty(&value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))

	require.NoError(t, <-result)
	live, err := repo.New(ti.conn).ListHeadersByServerID(ctx, uuid.MustParse(server.ID))
	require.NoError(t, err)
	require.Empty(t, live, "no live header may outlive its deleted server")
}

// A header write waiting on a server that is deleted meanwhile reports the
// server as gone rather than creating an orphan.
func TestCreateServerHeader_ServerDeletedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	project := projectID(t, ctx)

	holder, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return holder.Rollback(context.Background()) })

	_, err = repo.New(holder).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: uuid.MustParse(server.ID), ProjectID: project})
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
			p.Value = new("contender")
		}))
		result <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(holder), "%FROM remote_mcp_servers%FOR UPDATE%")

	_, err = repo.New(holder).DeleteServer(ctx, repo.DeleteServerParams{ID: uuid.MustParse(server.ID), ProjectID: project})
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))

	requireOopsCode(t, <-result, oops.CodeNotFound)
	live, err := repo.New(ti.conn).ListHeadersByServerID(ctx, uuid.MustParse(server.ID))
	require.NoError(t, err)
	require.Empty(t, live)
}
