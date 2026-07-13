package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateServerHeader_Secret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderCreate)
	require.NoError(t, err)

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
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
	require.NotNil(t, header.Description)
	require.Equal(t, "API key for authentication", *header.Description)
	require.Nil(t, header.ValueFromRequestHeader)

	// The plaintext must never come back out.
	require.NotNil(t, header.Value)
	require.Equal(t, "***", *header.Value)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateServerHeader_PassThrough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Request-ID", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Request-ID")
	}))
	require.NoError(t, err)

	require.False(t, header.IsSecret)
	require.False(t, header.IsRequired)
	require.Nil(t, header.Value)
	require.NotNil(t, header.ValueFromRequestHeader)
	require.Equal(t, "X-Request-ID", *header.ValueFromRequestHeader)
}

func TestCreateServerHeader_BothValuesRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "Bad-Header", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("static-value")
		p.ValueFromRequestHeader = new("X-Original")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_NeitherValueRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "Bad-Header", nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServerHeader_SecretPassThroughRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Trace-ID", func(p *gen.CreateServerHeaderPayload) {
		p.IsSecret = new(true)
		p.ValueFromRequestHeader = new("X-Trace-ID")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A live name collision must surface as a conflict, not a 500 and not a silent
// overwrite of the existing header.
func TestCreateServerHeader_DuplicateNameConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("first")
	}))
	require.NoError(t, err)

	_, err = ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("second")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

// The unique index is partial (WHERE deleted IS FALSE), so reusing the name of
// a soft-deleted header must succeed rather than conflict.
func TestCreateServerHeader_NameReusableAfterDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	first, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("first")
	}))
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               first.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	second, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("second")
	}))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
}

func TestCreateServerHeader_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(uuid.NewString(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A server in another project must not be addressable, even with a valid id.
func TestCreateServerHeader_OtherProjectServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherServer := seedOtherProjectServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(otherServer.ID.String(), "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A read-only grant must not satisfy createServerHeader's write scope.
func TestCreateServerHeader_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	requireOopsCode(t, err, oops.CodeForbidden)
}
