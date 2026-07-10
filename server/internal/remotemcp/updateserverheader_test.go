package remotemcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// createSecretHeader creates a secret header through the service so its value
// is genuinely encrypted at rest.
func createSecretHeader(t *testing.T, ctx context.Context, ti *testInstance, serverID string, name string, value string) *types.RemoteMcpServerHeader {
	t.Helper()

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(serverID, name, func(p *gen.CreateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.Value = new(value)
	}))
	require.NoError(t, err)

	return header
}

func TestUpdateServerHeader_ReplacesFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Description = new("original")
		p.IsRequired = new(true)
		p.Value = new("original-value")
	}))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key-v2", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("new-value")
	}))
	require.NoError(t, err)

	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "X-API-Key-v2", updated.Name)
	require.NotNil(t, updated.Value)
	require.Equal(t, "new-value", *updated.Value)

	// Omitted optional fields are reset: this is a full replace, not a patch.
	require.Nil(t, updated.Description)
	require.False(t, updated.IsRequired)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// Omitting value on a header that is already secret preserves the stored value
// rather than clearing it (which the CHECK constraint would reject anyway).
func TestUpdateServerHeader_PreservesExistingSecretValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "X-API-Key", "my-secret-value")

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.Description = new("now documented")
	}))
	require.NoError(t, err)
	require.True(t, updated.IsSecret)
	require.NotNil(t, updated.Value)
	require.Equal(t, "***", *updated.Value)

	// The stored ciphertext must still decrypt to the original plaintext. The
	// proxy read path is what actually consumes it, so read it unredacted.
	requireStoredSecretValue(t, ctx, ti, server.ID, "X-API-Key", "my-secret-value")
}

// Preserving a secret must not double-encrypt: a second no-value update still
// leaves the original plaintext recoverable.
func TestUpdateServerHeader_PreserveIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "X-API-Key", "my-secret-value")

	for range 3 {
		_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
			p.IsSecret = new(true)
		}))
		require.NoError(t, err)
	}

	requireStoredSecretValue(t, ctx, ti, server.ID, "X-API-Key", "my-secret-value")
}

func TestUpdateServerHeader_RotatesSecretValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "X-API-Key", "old-secret")

	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.Value = new("new-secret")
	}))
	require.NoError(t, err)

	requireStoredSecretValue(t, ctx, ti, server.ID, "X-API-Key", "new-secret")
}

// Marking an existing non-secret header secret without supplying a value is a
// 400, not a 500: there is no stored value to preserve.
func TestUpdateServerHeader_NewSecretWithoutValueRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("plain")
	}))
	require.NoError(t, err)

	_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.IsSecret = new(true)
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Switching a static header to a pass-through must satisfy the CHECK
// constraint: value is cleared in the same statement that sets the source.
func TestUpdateServerHeader_StaticToPassThrough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Trace-ID", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("static")
	}))
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-Trace-ID", func(p *gen.UpdateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Trace-ID")
	}))
	require.NoError(t, err)
	require.Nil(t, updated.Value)
	require.NotNil(t, updated.ValueFromRequestHeader)
	require.Equal(t, "X-Trace-ID", *updated.ValueFromRequestHeader)
}

// The reverse transition must also hold.
func TestUpdateServerHeader_PassThroughToStatic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Trace-ID", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Trace-ID")
	}))
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-Trace-ID", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("static")
	}))
	require.NoError(t, err)
	require.Nil(t, updated.ValueFromRequestHeader)
	require.NotNil(t, updated.Value)
	require.Equal(t, "static", *updated.Value)
}

// Switching a secret header to a pass-through clears the stored secret.
func TestUpdateServerHeader_SecretToPassThrough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "Authorization", "Bearer token")

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "Authorization", func(p *gen.UpdateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Authorization")
	}))
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.Nil(t, updated.Value)
	require.NotNil(t, updated.ValueFromRequestHeader)
}

func TestUpdateServerHeader_BothValuesRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("value")
		p.ValueFromRequestHeader = new("X-Original")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Switching a static header to environment-sourced persists the empty-value
// sentinel rather than NULLing value (which the CHECK would reject without a
// request-header source).
func TestUpdateServerHeader_StaticToEnvSourced(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("static")
	}))
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-Api-Key", nil))
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.Nil(t, updated.ValueFromRequestHeader)
	require.NotNil(t, updated.Value)
	require.Empty(t, *updated.Value)

	requireStoredEnvSourcedValue(t, ctx, ti, server.ID, "X-Api-Key")
}

func TestUpdateServerHeader_PassThroughToEnvSourced(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Api-Key", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Inbound")
	}))
	require.NoError(t, err)

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-Api-Key", nil))
	require.NoError(t, err)
	require.NotNil(t, updated.Value)
	require.Empty(t, *updated.Value)
	require.Nil(t, updated.ValueFromRequestHeader)

	requireStoredEnvSourcedValue(t, ctx, ti, server.ID, "X-Api-Key")
}

func TestUpdateServerHeader_SecretToEnvSourced(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "Authorization", "Bearer token")

	updated, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "Authorization", nil))
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.NotNil(t, updated.Value)
	require.Empty(t, *updated.Value)

	requireStoredEnvSourcedValue(t, ctx, ti, server.ID, "Authorization")
}

// Renaming onto a sibling header's name must conflict, not 500.
func TestUpdateServerHeader_RenameOntoExistingNameConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-First", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("one")
	}))
	require.NoError(t, err)

	second, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Second", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("two")
	}))
	require.NoError(t, err)

	_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(second.ID, "X-First", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("two")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestUpdateServerHeader_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(uuid.NewString(), "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A read-only grant must not satisfy updateServerHeader's write scope.
func TestUpdateServerHeader_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(created.ID, "X-API-Key", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("hijacked")
	}))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A header in another project must not be updatable by id.
func TestUpdateServerHeader_OtherProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherServer := seedOtherProjectServer(t, ctx, ti)

	otherHeader := remotemcptest.SeedHeader(t, ctx, ti.conn, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      otherServer.ID,
		ProjectID:              otherServer.ProjectID,
		Name:                   "X-Other",
		Description:            pgtype.Text{String: "", Valid: false},
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.ToPGText("other-value"),
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})

	_, err := ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(otherHeader.ID.String(), "X-Hijacked", func(p *gen.UpdateServerHeaderPayload) {
		p.Value = new("hijacked")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}
