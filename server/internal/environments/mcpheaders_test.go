package environments_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Synthetic values; assertions compare against these only.
const (
	syntheticHeaderValue = "synthetic-instance-url"
	syntheticSecretValue = "synthetic-secret-token"
)

func createEnvironment(t *testing.T, ctx context.Context, ti *testInstance, name string, entries ...*gen.EnvironmentEntryInput) *types.Environment {
	t.Helper()
	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             name,
		Description:      nil,
		Entries:          entries,
	})
	require.NoError(t, err)
	return env
}

func mustProjectID(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}

func statusesByEntry(env environments.MCPHeaderEnvironment) map[string]proxy.EnvironmentHeaderStatus {
	out := make(map[string]proxy.EnvironmentHeaderStatus, len(env.Headers))
	for _, h := range env.Headers {
		out[h.EntryName] = h.Status
	}
	return out
}

func TestInspectMCPHeaders_ReadsOnlyPrefixedEntries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	projectID := mustProjectID(t, ctx)
	env := createEnvironment(t, ctx, ti, "mcp-headers-prefixed",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Instance-Url", Value: new(syntheticHeaderValue), IsSecret: new(false)},
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_Authorization", Value: new(syntheticSecretValue), IsSecret: new(true)},
		&gen.EnvironmentEntryInput{Name: "API_KEY", Value: new("synthetic-unrelated"), IsSecret: new(true)},
		&gen.EnvironmentEntryInput{Name: "mcp_header_X-Typo", Value: new("synthetic-unrelated"), IsSecret: new(true)},
	)
	// An unrelated secret that cannot be decrypted must not affect the read:
	// only MCP_HEADER_ entries are decrypted.
	_, err := repo.New(ti.conn).CreateEnvironmentEntries(ctx, repo.CreateEnvironmentEntriesParams{
		EnvironmentID: uuid.MustParse(env.ID),
		Names:         []string{"BROKEN_SECRET"},
		Values:        []string{"not-ciphertext"},
		IsSecrets:     []bool{true},
	})
	require.NoError(t, err)

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, projectID, uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Equal(t, env.ID, got.ID.String())
	require.Equal(t, env.Name, got.Name)
	require.Equal(t, map[string]proxy.EnvironmentHeaderStatus{
		"MCP_HEADER_Authorization":  proxy.EnvironmentHeaderMapped,
		"MCP_HEADER_X-Instance-Url": proxy.EnvironmentHeaderMapped,
	}, statusesByEntry(got))

	rows, err := proxy.EnvironmentHeaderRows(got.Headers)
	require.NoError(t, err)
	values := map[string]string{}
	for _, row := range rows {
		values[row.Name] = row.StaticValue
	}
	require.Equal(t, map[string]string{"Authorization": syntheticSecretValue, "X-Instance-Url": syntheticHeaderValue}, values)
}

func TestInspectMCPHeaders_LiveEnvironmentWithoutMappedEntries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-none",
		&gen.EnvironmentEntryInput{Name: "API_KEY", Value: new("synthetic-unrelated"), IsSecret: new(true)},
	)

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Equal(t, env.ID, got.ID.String())
	require.Empty(t, got.Headers)
}

func TestInspectMCPHeaders_EmptyEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-empty")

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Empty(t, got.Headers)
}

func TestInspectMCPHeaders_DeletedEnvironmentIsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-deleted",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Instance-Url", Value: new(syntheticHeaderValue), IsSecret: new(false)},
	)
	require.NoError(t, ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{Slug: env.Slug, SessionToken: nil, ProjectSlugInput: nil}))

	_, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.MustParse(env.ID))
	require.ErrorIs(t, err, environments.ErrEnvironmentUnavailable)
}

func TestInspectMCPHeaders_MissingEnvironmentIsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	_, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.New())
	require.ErrorIs(t, err, environments.ErrEnvironmentUnavailable)
}

func TestInspectMCPHeaders_OtherProjectIsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-foreign",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Instance-Url", Value: new(syntheticHeaderValue), IsSecret: new(false)},
	)

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, uuid.New(), uuid.MustParse(env.ID))
	require.ErrorIs(t, err, environments.ErrEnvironmentUnavailable)
	require.Empty(t, got.Name)
	require.Empty(t, got.Headers)
}

// Values left empty by a clone without values are classified, not sent.
func TestInspectMCPHeaders_CloneWithoutValuesIsEmptyValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	source := createEnvironment(t, ctx, ti, "mcp-headers-clone-source",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Instance-Url", Value: new(syntheticHeaderValue), IsSecret: new(true)},
	)
	clone, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "mcp-headers-clone-target",
		CopyValues:       nil,
	})
	require.NoError(t, err)

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.MustParse(clone.ID))
	require.NoError(t, err)
	require.Equal(t, map[string]proxy.EnvironmentHeaderStatus{"MCP_HEADER_X-Instance-Url": proxy.EnvironmentHeaderEmptyValue}, statusesByEntry(got))
	_, err = proxy.EnvironmentHeaderRows(got.Headers)
	require.ErrorIs(t, err, proxy.ErrInvalidEnvironmentHeader)
}

func TestInspectMCPHeaders_UndecryptableMappedEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-undecryptable",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Ok", Value: new(syntheticHeaderValue), IsSecret: new(false)},
	)
	_, err := repo.New(ti.conn).CreateEnvironmentEntries(ctx, repo.CreateEnvironmentEntriesParams{
		EnvironmentID: uuid.MustParse(env.ID),
		Names:         []string{"MCP_HEADER_X-Broken"},
		Values:        []string{"not-ciphertext"},
		IsSecrets:     []bool{true},
	})
	require.NoError(t, err)

	got, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil).InspectMCPHeaders(ctx, mustProjectID(t, ctx), uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Equal(t, map[string]proxy.EnvironmentHeaderStatus{
		"MCP_HEADER_X-Broken": proxy.EnvironmentHeaderUndecryptable,
		"MCP_HEADER_X-Ok":     proxy.EnvironmentHeaderMapped,
	}, statusesByEntry(got))
	_, err = proxy.EnvironmentHeaderRows(got.Headers)
	require.ErrorIs(t, err, proxy.ErrUndecryptableEnvironmentHeader)
	require.NotContains(t, err.Error(), "not-ciphertext")
}

func TestMCPHeaderNearMissNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	env := createEnvironment(t, ctx, ti, "mcp-headers-near-miss",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Ok", Value: new(syntheticHeaderValue), IsSecret: new(false)},
		&gen.EnvironmentEntryInput{Name: "mcp_header_X-Lower", Value: new("synthetic-unrelated"), IsSecret: new(true)},
		&gen.EnvironmentEntryInput{Name: "HEADER_X-Short", Value: new("synthetic-unrelated"), IsSecret: new(true)},
		&gen.EnvironmentEntryInput{Name: "API_KEY", Value: new("synthetic-unrelated"), IsSecret: new(true)},
	)

	entries := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil)
	names, err := entries.MCPHeaderNearMissNames(ctx, mustProjectID(t, ctx), uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Equal(t, []string{"HEADER_X-Short", "mcp_header_X-Lower"}, names)

	foreign, err := entries.MCPHeaderNearMissNames(ctx, uuid.New(), uuid.MustParse(env.ID))
	require.NoError(t, err)
	require.Empty(t, foreign)
}

func seedRemote(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, url string) uuid.UUID {
	t.Helper()
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{}, Slug: pgtype.Text{}, TransportType: "streamable-http", Url: url,
	})
	require.NoError(t, err)
	return remote.ID
}

func updateServerLink(t *testing.T, ctx context.Context, ti *testInstance, server mcpserversrepo.McpServer, remoteID uuid.UUID, environmentID uuid.NullUUID) {
	t.Helper()
	_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name: server.Name, Slug: server.Slug, EnvironmentID: environmentID, UserSessionIssuerID: server.UserSessionIssuerID,
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: server.Visibility, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)
}

// The snapshot reads the server's backend, remote URL, link and mapped
// entries together, and reflects each concurrent change a serving request
// must detect.
func TestInspectMCPServerHeaders_SnapshotTracksBackendURLAndLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	projectID := mustProjectID(t, ctx)
	env := createEnvironment(t, ctx, ti, "mcp-headers-snapshot",
		&gen.EnvironmentEntryInput{Name: "MCP_HEADER_X-Instance-Url", Value: new(syntheticSecretValue), IsSecret: new(true)},
		&gen.EnvironmentEntryInput{Name: "UNRELATED", Value: new("synthetic-unrelated"), IsSecret: new(true)},
	)
	envID := uuid.NullUUID{UUID: uuid.MustParse(env.ID), Valid: true}
	remoteA := seedRemote(t, ctx, ti, projectID, "https://a.example.invalid/mcp")
	remoteB := seedRemote(t, ctx, ti, projectID, "https://b.example.invalid/mcp")
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "snap", Valid: true}, Slug: pgtype.Text{String: "snap-" + uuid.NewString()[:8], Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteA, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	entries := environments.NewEnvironmentEntries(testenv.NewLogger(t), ti.conn, ti.enc, nil)

	snap, err := entries.InspectMCPServerHeaders(ctx, projectID, server.ID)
	require.NoError(t, err)
	require.False(t, snap.EnvironmentID.Valid)
	require.Equal(t, remoteA, snap.RemoteMcpServerID.UUID)
	require.Equal(t, "https://a.example.invalid/mcp", snap.RemoteURL)
	require.Empty(t, snap.Headers)

	// Repointed and linked in one update.
	updateServerLink(t, ctx, ti, server, remoteB, envID)
	snap, err = entries.InspectMCPServerHeaders(ctx, projectID, server.ID)
	require.NoError(t, err)
	require.Equal(t, envID, snap.EnvironmentID)
	require.True(t, snap.EnvironmentLive)
	require.Equal(t, remoteB, snap.RemoteMcpServerID.UUID)
	require.Equal(t, "https://b.example.invalid/mcp", snap.RemoteURL)
	require.Len(t, snap.Headers, 1)
	require.Equal(t, "MCP_HEADER_X-Instance-Url", snap.Headers[0].EntryName)

	// Unlinked, then the source URL moved.
	updateServerLink(t, ctx, ti, server, remoteB, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err = remotemcprepo.New(ti.conn).UpdateServer(ctx, remotemcprepo.UpdateServerParams{
		Name: pgtype.Text{}, Slug: pgtype.Text{}, TransportType: "streamable-http", Url: "https://moved.example.invalid/mcp", ID: remoteB, ProjectID: projectID,
	})
	require.NoError(t, err)
	snap, err = entries.InspectMCPServerHeaders(ctx, projectID, server.ID)
	require.NoError(t, err)
	require.False(t, snap.EnvironmentID.Valid)
	require.Equal(t, "https://moved.example.invalid/mcp", snap.RemoteURL)
	require.Empty(t, snap.Headers)

	// A deleted linked environment keeps the link but is not live.
	updateServerLink(t, ctx, ti, server, remoteB, envID)
	require.NoError(t, ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{Slug: env.Slug, SessionToken: nil, ProjectSlugInput: nil}))
	snap, err = entries.InspectMCPServerHeaders(ctx, projectID, server.ID)
	require.NoError(t, err)
	require.Equal(t, envID, snap.EnvironmentID)
	require.False(t, snap.EnvironmentLive)
	require.Empty(t, snap.Headers)

	// Another project sees no server.
	_, err = entries.InspectMCPServerHeaders(ctx, uuid.New(), server.ID)
	require.ErrorIs(t, err, environments.ErrMCPServerUnavailable)
}
