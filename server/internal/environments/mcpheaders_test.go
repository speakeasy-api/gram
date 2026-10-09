package environments_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
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
