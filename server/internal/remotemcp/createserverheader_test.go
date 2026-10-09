package remotemcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// The proxy never reads these sources, so a row naming one is refused at
// write time rather than stored as a header that can never be populated.
func TestCreateServerHeader_DeniedPassThroughSourceRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Session", func(p *gen.CreateServerHeaderPayload) {
		p.IsSecret = new(false)
		p.ValueFromRequestHeader = new("Cookie")
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

func projectID(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}

// seedLegacyHeader stores a header exactly as given, bypassing write
// validation, the way rows written before the remote header policy look.
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

	for _, source := range []string{"Gram-Key", "gRaM-cHaT-sEsSiOn", "Gram_Session", "Gram-Project", "Gram-Consent-State", "X-Gram-Tunnel-Id", "X-Gram-Agent-Version", "X_Speakeasy_Identity", "Proxy-Authorization", "Set-Cookie", "Speakeasy-AI-Key", "speakeasy-ai-chat-session"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
			p.ValueFromRequestHeader = new(source)
		}))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Gram-Key")
	}))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Contains(t, oopsErr.Error(), "never forwarded to remote MCP servers", "the refusal says why")
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

	// Forwarding the caller's own upstream credential is what pass-through
	// identity is for.
	header, err = ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Upstream-Token", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Authorization")
	}))
	require.NoError(t, err)
	require.Equal(t, "Authorization", *header.ValueFromRequestHeader)
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

func TestCreateServerHeader_StoresNamesAsEntered(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, " x-forwarded-token ", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("x-caller-token")
	}))
	require.NoError(t, err)
	require.Equal(t, "x-forwarded-token", header.Name)
	require.Equal(t, "x-caller-token", *header.ValueFromRequestHeader)
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

// A header write locks the parent server and checks for a case-insensitive
// duplicate only after it holds the lock, so a duplicate committed by a writer
// that held the lock first is seen.
func TestCreateServerHeader_DuplicateCheckWaitsForServerLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	project := projectID(t, ctx)

	holder := testenv.BeginTx(t, ctx, ti.conn)

	_, err := repo.New(holder).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: uuid.MustParse(server.ID), ProjectID: project})
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

// A header write waiting on a server that is deleted meanwhile reports the
// server as gone rather than creating an orphan.
func TestCreateServerHeader_ServerDeletedWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	project := projectID(t, ctx)

	holder := testenv.BeginTx(t, ctx, ti.conn)

	_, err := repo.New(holder).GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: uuid.MustParse(server.ID), ProjectID: project})
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

// Underscores read as dashes when names are compared, as the header policy and
// some upstreams read them, so X_Api_Key duplicates X-Api-Key.
func TestCreateServerHeader_UnderscoreDuplicateConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	seedLegacyHeader(t, ctx, ti, server.ID, "X-Api-Key", "legacy", "")

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "x_api_key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("new")
	}))
	requireOopsCode(t, err, oops.CodeConflict)
}

// The write-time refusal is the only explanation an operator gets, so each
// one names the header and what to do instead.
func TestCreateServerHeader_RefusalsExplainTheFix(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Taken", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("v")
	}))
	require.NoError(t, err)

	cases := []struct {
		name string
		opts func(*gen.CreateServerHeaderPayload)
		code oops.Code
		want []string
	}{
		{name: "X-Upstream-Token", opts: func(p *gen.CreateServerHeaderPayload) { p.ValueFromRequestHeader = new("gram-key") }, code: oops.CodeBadRequest, want: []string{`header "X-Upstream-Token" cannot read request header "gram-key": Speakeasy headers are never forwarded to remote MCP servers`}},
		{name: "set-cookie", opts: func(p *gen.CreateServerHeaderPayload) { p.Value = new("v") }, code: oops.CodeBadRequest, want: []string{`header "set-cookie" cannot be configured on a remote MCP server`, "Cookie can only hold a static value"}},
		{name: "X Bad", opts: func(p *gen.CreateServerHeaderPayload) { p.Value = new("v") }, code: oops.CodeBadRequest, want: []string{`header name "X Bad" is not a valid HTTP header name`}},
		{name: "X-Forwarded", opts: func(p *gen.CreateServerHeaderPayload) { p.ValueFromRequestHeader = new("X Bad") }, code: oops.CodeBadRequest, want: []string{`header "X-Forwarded" reads request header "X Bad", which is not a valid HTTP header name`}},
		{name: "X-Api-Key", opts: func(p *gen.CreateServerHeaderPayload) { p.Value = new("line1\nline2") }, code: oops.CodeBadRequest, want: []string{`the value of header "X-Api-Key" contains a character an HTTP header cannot carry`}},
		{name: "x_taken", opts: func(p *gen.CreateServerHeaderPayload) { p.Value = new("v") }, code: oops.CodeConflict, want: []string{`this server already has a header named "x_taken"`, "'_' matches '-'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, tc.name, tc.opts))
			requireOopsCode(t, err, tc.code)
			for _, want := range tc.want {
				require.Contains(t, err.Error(), want)
			}
			require.NotContains(t, err.Error(), "line1", "the refusal never echoes a value")
		})
	}
}
