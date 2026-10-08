package tunneledmcp

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// syntheticSecret is a fake credential; tests compare against it, never print it.
const syntheticSecret = "synthetic-tunnel-secret"

func createHeaderPayload(serverID uuid.UUID, name string) *gen.CreateServerHeaderPayload {
	return &gen.CreateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		TunneledMcpServerID:    serverID.String(),
		Name:                   name,
		Description:            nil,
		IsRequired:             nil,
		IsSecret:               nil,
		Value:                  new("static-value"),
		ValueFromRequestHeader: nil,
	}
}

func updateHeaderPayload(id string, name string) *gen.UpdateServerHeaderPayload {
	return &gen.UpdateServerHeaderPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		ID:                     id,
		Name:                   name,
		Description:            nil,
		IsRequired:             nil,
		IsSecret:               nil,
		Value:                  nil,
		ValueFromRequestHeader: nil,
	}
}

func getHeader(t *testing.T, ctx context.Context, ti *testInstance, id string) *types.TunneledMcpServerHeader {
	t.Helper()
	header, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id})
	require.NoError(t, err)
	return header
}

func configuredValue(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID, name string) (string, bool) {
	t.Helper()
	headers, err := NewHeaders(ti.service.logger, ti.conn, ti.service.enc).ConfiguredHeaders(ctx, serverID)
	require.NoError(t, err)
	for _, h := range headers {
		if h.Name == name {
			return h.StaticValue, true
		}
	}
	return "", false
}

func TestServerHeaderCRUD(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	payload := createHeaderPayload(server.ID, "x-jamf-tenant")
	payload.Description = new("JAMF tenant")
	payload.IsRequired = new(true)
	created, err := ti.service.CreateServerHeader(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "X-Jamf-Tenant", created.Name, "names are stored canonically")
	require.Equal(t, "static-value", *created.Value)
	require.True(t, created.IsRequired)

	list, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, TunneledMcpServerID: server.ID.String()})
	require.NoError(t, err)
	require.Len(t, list.Headers, 1)
	require.Equal(t, created.ID, list.Headers[0].ID)

	update := updateHeaderPayload(created.ID, "X-Jamf-Region")
	update.ValueFromRequestHeader = new("x-client-region")
	updated, err := ti.service.UpdateServerHeader(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "X-Jamf-Region", updated.Name)
	require.Nil(t, updated.Value)
	require.Equal(t, "X-Client-Region", *updated.ValueFromRequestHeader)
	require.False(t, updated.IsRequired, "update replaces every mutable field")

	err = ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID})
	require.NoError(t, err)

	_, err = ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)

	// A deleted name can be reused.
	_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Jamf-Region"))
	require.NoError(t, err)
}

func TestServerHeaderSecretLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	payload := createHeaderPayload(server.ID, "X-Api-Key")
	payload.IsSecret = new(true)
	payload.Value = new(syntheticSecret)
	created, err := ti.service.CreateServerHeader(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "***", *created.Value)
	require.Equal(t, "***", *getHeader(t, ctx, ti, created.ID).Value)

	stored, err := repo.New(ti.conn).GetServerHeader(ctx, repo.GetServerHeaderParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.NotEqual(t, syntheticSecret, stored.Value.String, "secret values are encrypted at rest")

	value, ok := configuredValue(t, ctx, ti, server.ID, "X-Api-Key")
	require.True(t, ok)
	require.Equal(t, syntheticSecret, value, "the proxy receives the decrypted value")

	// Omitting the value of an existing secret keeps it.
	keep := updateHeaderPayload(created.ID, "X-Api-Key")
	keep.IsSecret = new(true)
	keep.Description = new("renamed description")
	_, err = ti.service.UpdateServerHeader(ctx, keep)
	require.NoError(t, err)
	value, _ = configuredValue(t, ctx, ti, server.ID, "X-Api-Key")
	require.Equal(t, syntheticSecret, value)

	// Turning the secret into a plain header requires a new value.
	plain := updateHeaderPayload(created.ID, "X-Api-Key")
	_, err = ti.service.UpdateServerHeader(ctx, plain)
	requireOopsCode(t, err, oops.CodeBadRequest)

	plain.Value = new("now-public")
	updated, err := ti.service.UpdateServerHeader(ctx, plain)
	require.NoError(t, err)
	require.False(t, updated.IsSecret)
	require.Equal(t, "now-public", *updated.Value)

	// A plain header cannot preserve a value it never had as a secret.
	reSecret := updateHeaderPayload(created.ID, "X-Api-Key")
	reSecret.IsSecret = new(true)
	_, err = ti.service.UpdateServerHeader(ctx, reSecret)
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Switching to a pass-through source clears the stored value.
	passThrough := updateHeaderPayload(created.ID, "X-Api-Key")
	passThrough.ValueFromRequestHeader = new("X-Client-Key")
	updated, err = ti.service.UpdateServerHeader(ctx, passThrough)
	require.NoError(t, err)
	require.Nil(t, updated.Value)

	secretPassThrough := updateHeaderPayload(created.ID, "X-Api-Key")
	secretPassThrough.IsSecret = new(true)
	secretPassThrough.ValueFromRequestHeader = new("X-Client-Key")
	_, err = ti.service.UpdateServerHeader(ctx, secretPassThrough)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestServerHeaderAuditIsRedacted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	payload := createHeaderPayload(server.ID, "X-Api-Key")
	payload.IsSecret = new(true)
	payload.Value = new(syntheticSecret)
	created, err := ti.service.CreateServerHeader(ctx, payload)
	require.NoError(t, err)

	createRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderCreate)
	require.NoError(t, err)
	require.Equal(t, created.ID, createRecord.SubjectID)
	require.Equal(t, "X-Api-Key", createRecord.SubjectDisplay)

	update := updateHeaderPayload(created.ID, "X-Api-Key")
	update.IsSecret = new(true)
	update.Value = new(syntheticSecret + "-rotated")
	_, err = ti.service.UpdateServerHeader(ctx, update)
	require.NoError(t, err)

	updateRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderUpdate)
	require.NoError(t, err)
	require.NotContains(t, string(updateRecord.BeforeSnapshot), syntheticSecret)
	require.NotContains(t, string(updateRecord.AfterSnapshot), syntheticSecret)
	require.Contains(t, string(updateRecord.AfterSnapshot), "***")

	stored, err := repo.New(ti.conn).GetServerHeader(ctx, repo.GetServerHeaderParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.NotContains(t, string(updateRecord.BeforeSnapshot), stored.Value.String, "ciphertext never reaches the audit log")

	err = ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID})
	require.NoError(t, err)
	deleteRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderDelete)
	require.NoError(t, err)
	require.Equal(t, created.ID, deleteRecord.SubjectID)
	require.Empty(t, deleteRecord.BeforeSnapshot)
}

func TestServerHeaderWriteRollsBackWhenAuditFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderCreate))

	_, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Tenant"))
	requireOopsCode(t, err, oops.CodeUnexpected)

	count, err := repo.New(ti.conn).CountLiveServerHeaders(ctx, server.ID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestServerHeaderCaseInsensitiveConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	first, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Tenant"))
	require.NoError(t, err)
	_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "x-tenant"))
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, " X-TENANT "))
	requireOopsCode(t, err, oops.CodeConflict)

	second, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Region"))
	require.NoError(t, err)
	rename := updateHeaderPayload(second.ID, "x-TENANT")
	rename.Value = new("v")
	_, err = ti.service.UpdateServerHeader(ctx, rename)
	requireOopsCode(t, err, oops.CodeConflict)

	// Renaming a header to its own name in another case is not a conflict.
	self := updateHeaderPayload(first.ID, "x-tenant")
	self.Value = new("v")
	updated, err := ti.service.UpdateServerHeader(ctx, self)
	require.NoError(t, err)
	require.Equal(t, "X-Tenant", updated.Name)

	// The same name on another tunnel is fine.
	other := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(other.ID, "X-Tenant"))
	require.NoError(t, err)
}

func TestServerHeaderConcurrentCreatesOfOneNameConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	names := []string{"x-shared", "X-SHARED", "X-Shared", "x-Shared"}
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			_, errs[i] = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, name))
		})
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		requireOopsCode(t, err, oops.CodeConflict)
	}
	require.Equal(t, 1, succeeded)
}

func TestServerHeaderCreateRacingServerDeleteLeavesNoLiveHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)

	for range 5 {
		server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
		var wg sync.WaitGroup
		wg.Go(func() {
			_, _ = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Tenant"))
		})
		wg.Go(func() {
			require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: server.ID.String()}))
		})
		wg.Wait()

		count, err := repo.New(ti.conn).CountLiveServerHeaders(ctx, server.ID)
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestDeleteServerCascadesAndAuditsHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	a, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-A"))
	require.NoError(t, err)
	b, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-B"))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: server.ID.String()}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionTunneledMcpServerHeaderDelete)
	require.NoError(t, err)
	require.Equal(t, before+2, after)

	count, err := repo.New(ti.conn).CountLiveServerHeaders(ctx, server.ID)
	require.NoError(t, err)
	require.Zero(t, count)

	for _, id := range []string{a.ID, b.ID} {
		_, err = ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id})
		requireOopsCode(t, err, oops.CodeNotFound)
	}
}

func TestServerHeaderPolicyRejections(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	tests := []struct {
		name   string
		header string
		value  *string
		source *string
	}{
		{name: "assertion header", header: "X-Speakeasy-Identity", value: new("x")},
		{name: "assertion underscore alias", header: "x_speakeasy_identity", value: new("x")},
		{name: "tunnel forward token", header: "X-Gram-Tunnel-Forward-Token", value: new("x")},
		{name: "tunnel agent version", header: "X-Gram-Agent-Version", value: new("x")},
		{name: "speakeasy api key", header: "Gram-Key", value: new("x")},
		{name: "cookie", header: "Cookie", value: new("x")},
		{name: "set-cookie", header: "Set-Cookie", value: new("x")},
		{name: "mcp session", header: "Mcp-Session-Id", value: new("x")},
		{name: "mcp protocol version", header: "MCP-Protocol-Version", value: new("x")},
		{name: "mcp method", header: "Mcp-Method", value: new("x")},
		{name: "framing", header: "Transfer-Encoding", value: new("x")},
		{name: "host", header: "Host", value: new("x")},
		{name: "invalid name bytes", header: "X Tenant", value: new("x")},
		{name: "crlf name", header: "X-Tenant\r\nX-Injected", value: new("x")},
		{name: "crlf value", header: "X-Tenant", value: new("a\r\nX-Injected: 1")},
		{name: "nul value", header: "X-Tenant", value: new("a\x00b")},
		{name: "authorization source", header: "X-Upstream-Token", source: new("Authorization")},
		{name: "speakeasy key source", header: "X-Upstream-Token", source: new("gram-key")},
		{name: "chat session source", header: "X-Upstream-Token", source: new("Gram-Chat-Session")},
		{name: "cookie source", header: "X-Upstream-Token", source: new("Cookie")},
		{name: "assertion alias source", header: "X-Upstream-Token", source: new("X_Speakeasy_Identity")},
		{name: "invalid source name", header: "X-Upstream-Token", source: new("X Client")},
		{name: "no value or source", header: "X-Tenant"},
		{name: "both value and source", header: "X-Tenant", value: new("x"), source: new("X-Client")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := createHeaderPayload(server.ID, tc.header)
			payload.Value = tc.value
			payload.ValueFromRequestHeader = tc.source
			_, err := ti.service.CreateServerHeader(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	// Authorization is allowed as a static destination.
	auth := createHeaderPayload(server.ID, "authorization")
	auth.IsSecret = new(true)
	auth.Value = new("Basic " + syntheticSecret)
	created, err := ti.service.CreateServerHeader(ctx, auth)
	require.NoError(t, err)
	require.Equal(t, "Authorization", created.Name)

	// Updates get the same policy.
	update := updateHeaderPayload(created.ID, "X-Gram-Tunnel-Id")
	update.Value = new("x")
	_, err = ti.service.UpdateServerHeader(ctx, update)
	requireOopsCode(t, err, oops.CodeBadRequest)

	count, err := repo.New(ti.conn).CountLiveServerHeaders(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestServerHeaderTenantBoundaries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)

	projects := map[string]string{
		"same organization":  authCtx.ActiveOrganizationID,
		"other organization": "org-" + uuid.NewString(),
	}
	for label, orgID := range projects {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			slug := "other-" + uuid.NewString()[:8]
			other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: orgID})
			require.NoError(t, err)
			foreignServer := seedTunneledMcpServer(t, ctx, ti.conn, other.ID)
			foreignHeader, err := repo.New(ti.conn).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
				Name:                   "X-Foreign",
				Description:            pgtype.Text{String: "", Valid: false},
				IsRequired:             false,
				IsSecret:               false,
				Value:                  conv.ToPGText("foreign"),
				ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
				TunneledMcpServerID:    foreignServer.ID,
				ProjectID:              other.ID,
			})
			require.NoError(t, err)

			_, err = ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, TunneledMcpServerID: foreignServer.ID.String()})
			requireOopsCode(t, err, oops.CodeNotFound)
			_, err = ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: foreignHeader.ID.String()})
			requireOopsCode(t, err, oops.CodeNotFound)
			_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(foreignServer.ID, "X-Tenant"))
			requireOopsCode(t, err, oops.CodeNotFound)
			update := updateHeaderPayload(foreignHeader.ID.String(), "X-Foreign")
			update.Value = new("hijacked")
			_, err = ti.service.UpdateServerHeader(ctx, update)
			requireOopsCode(t, err, oops.CodeNotFound)
			err = ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: foreignHeader.ID.String()})
			requireOopsCode(t, err, oops.CodeNotFound)

			count, err := repo.New(ti.conn).CountLiveServerHeaders(ctx, foreignServer.ID)
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
		})
	}

	_, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestServerHeaderOnDeletedServerIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	header, err := repo.New(ti.conn).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		Name:                   "X-Tenant",
		Description:            pgtype.Text{String: "", Valid: false},
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.ToPGText("v"),
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
		TunneledMcpServerID:    server.ID,
		ProjectID:              *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = repo.New(ti.conn).DeleteServer(ctx, repo.DeleteServerParams{ID: server.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	_, err = ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: header.ID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	update := updateHeaderPayload(header.ID.String(), "X-Tenant")
	update.Value = new("v2")
	_, err = ti.service.UpdateServerHeader(ctx, update)
	requireOopsCode(t, err, oops.CodeNotFound)
	err = ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: header.ID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-New"))
	requireOopsCode(t, err, oops.CodeNotFound)

	configured, err := NewHeaders(ti.service.logger, ti.conn, ti.service.enc).ConfiguredHeaders(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, configured)
}

func TestServerHeaderRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	created, err := ti.service.CreateServerHeader(ctx, createHeaderPayload(server.ID, "X-Tenant"))
	require.NoError(t, err)

	listPayload := &gen.ListServerHeadersPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, TunneledMcpServerID: server.ID.String()}
	getPayload := &gen.GetServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID}
	deletePayload := &gen.DeleteServerHeaderPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: created.ID}
	update := updateHeaderPayload(created.ID, "X-Tenant")
	update.Value = new("v2")

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err = ti.service.ListServerHeaders(noGrants, listPayload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetServerHeader(noGrants, getPayload)
	requireOopsCode(t, err, oops.CodeForbidden)

	readOnly := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPRead, *authCtx.ProjectID))
	_, err = ti.service.ListServerHeaders(readOnly, listPayload)
	require.NoError(t, err)
	_, err = ti.service.GetServerHeader(readOnly, getPayload)
	require.NoError(t, err)
	_, err = ti.service.CreateServerHeader(readOnly, createHeaderPayload(server.ID, "X-Other"))
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.UpdateServerHeader(readOnly, update)
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.DeleteServerHeader(readOnly, deletePayload)
	requireOopsCode(t, err, oops.CodeForbidden)

	otherProject := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, uuid.New()))
	_, err = ti.service.CreateServerHeader(otherProject, createHeaderPayload(server.ID, "X-Other"))
	requireOopsCode(t, err, oops.CodeForbidden)

	writer := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	_, err = ti.service.UpdateServerHeader(writer, update)
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteServerHeader(writer, deletePayload))
}
