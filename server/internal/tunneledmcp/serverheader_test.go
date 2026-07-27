package tunneledmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newCreateHeaderPayload(serverID string, name string, mutate func(*gen.CreateServerHeaderPayload)) *gen.CreateServerHeaderPayload {
	p := &gen.CreateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		TunneledMcpServerID:    serverID,
		Name:                   name,
		Description:            nil,
		IsRequired:             nil,
		IsSecret:               nil,
		Value:                  nil,
		ValueFromRequestHeader: nil,
	}
	if mutate != nil {
		mutate(p)
	}
	return p
}

func TestCreateServerHeader_SecretRedactedAndEncryptedAtRest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderCreate)
	require.NoError(t, err)

	header, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Description = new("API key for authentication")
		p.IsRequired = new(true)
		p.IsSecret = new(true)
		p.Value = new("secret-key-123")
	}))
	require.NoError(t, err)

	require.NotEmpty(t, header.ID)
	require.Equal(t, "X-API-Key", header.Name)
	require.True(t, header.IsSecret)
	require.True(t, header.IsRequired)
	require.Nil(t, header.ValueFromRequestHeader)
	// The plaintext must never come back out of the management API.
	require.NotNil(t, header.Value)
	require.Equal(t, "***", *header.Value)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The proxy path reads decrypted values. A round-trip through the header
	// loader must yield the original plaintext, proving the value was encrypted
	// at rest and can be decrypted for injection.
	revealed := loadServerHeaderValue(t, ctx, ti, server.ID, "X-API-Key")
	require.Equal(t, "secret-key-123", revealed)
}

func TestCreateServerHeader_PassThrough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	header, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-Request-ID", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Request-ID")
	}))
	require.NoError(t, err)

	require.False(t, header.IsSecret)
	require.Nil(t, header.Value)
	require.NotNil(t, header.ValueFromRequestHeader)
	require.Equal(t, "X-Request-ID", *header.ValueFromRequestHeader)
}

func TestCreateServerHeader_BothValuesRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "Bad", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("static")
		p.ValueFromRequestHeader = new("X-Original")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_NeitherValueRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "Bad", nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_SecretPassThroughRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-Trace", func(p *gen.CreateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.ValueFromRequestHeader = new("X-Trace")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_DuplicateNameConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("first")
	}))
	require.NoError(t, err)

	_, err = ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("second")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateServerHeader_NameReusableAfterDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	first, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("first")
	}))
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               first.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	second, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("second")
	}))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
}

func TestCreateServerHeader_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(uuid.NewString(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Omitting the value on an existing secret header preserves its stored value.
func TestUpdateServerHeader_PreservesSecretWhenValueOmitted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	created, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.Value = new("original-secret")
	}))
	require.NoError(t, err)

	// Update metadata only, leaving the value unset. The stored secret must
	// survive rather than being cleared (which would violate the value-source
	// constraint) or overwritten with the redaction placeholder.
	_, err = ti.service.UpdateServerHeader(ctx, &gen.UpdateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		ID:                     created.ID,
		Name:                   "X-API-Key",
		Description:            new("now with a description"),
		IsRequired:             new(true),
		IsSecret:               new(true),
		Value:                  nil,
		ValueFromRequestHeader: nil,
	})
	require.NoError(t, err)

	revealed := loadServerHeaderValue(t, ctx, ti, server.ID, "X-API-Key")
	require.Equal(t, "original-secret", revealed)
}

func TestListServerHeaders_ScopedToProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-One", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("1")
	}))
	require.NoError(t, err)

	listed, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		TunneledMcpServerID: server.ID.String(),
	})
	require.NoError(t, err)
	require.Len(t, listed.Headers, 1)
	require.Equal(t, "X-One", listed.Headers[0].Name)
}

// Deleting the server soft-deletes its headers so they no longer resolve.
func TestDeleteServer_CascadesHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.CreateServerHeader(ctx, newCreateHeaderPayload(server.ID.String(), "X-Cascade", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("v")
	}))
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               server.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	// The proxy loader is not project-scoped, so a surviving header would still
	// show up here — assert the cascade actually removed it.
	remaining, err := NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).ListHeaders(ctx, server.ID, false)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestCreateServerHeader_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	readOnly := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPRead, *authCtx.ProjectID))

	_, err := ti.service.CreateServerHeader(readOnly, newCreateHeaderPayload(server.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func loadServerHeaderValue(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID, name string) string {
	t.Helper()

	headers, err := NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).ListHeaders(ctx, serverID, false)
	require.NoError(t, err)
	for _, h := range headers {
		if h.Name == name {
			require.True(t, h.Value.Valid)
			return h.Value.String
		}
	}
	t.Fatalf("header %q not found", name)
	return ""
}
