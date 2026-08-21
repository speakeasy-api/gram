package unproxiedmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/unproxied_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	unproxiedmcprepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
)

func TestListTools_UnreachableServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	// Seeded directly through the repo rather than via CreateServer: a
	// server's URL can stop resolving after creation, so ListTools must
	// degrade gracefully even though CreateServer's own validation would now
	// reject this URL up front.
	//
	// unresolvableTestHost fails at the mocked DNS lookup itself (see
	// newUnproxiedMCPMockResolver), so the test is fast and deterministic
	// everywhere. A real unroutable domain like vendor.invalid would instead
	// fall through the mock resolver's default case to a real, uncontrolled
	// public IP — an actual network operation whose failure timing varies by
	// environment, which previously made this test take 80s+ locally and
	// nearly 3 minutes in CI.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := unproxiedmcprepo.New(ti.conn).CreateServer(ctx, unproxiedmcprepo.CreateServerParams{
		ID:          uuid.New(),
		ProjectID:   *authCtx.ProjectID,
		Name:        pgtype.Text{String: "", Valid: false},
		Slug:        pgtype.Text{String: "test-unreachable-" + uuid.NewString(), Valid: true},
		Url:         "https://" + unresolvableTestHost + "/mcp",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		ID:               created.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Status)
	require.Empty(t, result.Tools)
	require.NotNil(t, result.Message)
}

func TestListTools_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		ID:               "00000000-0000-0000-0000-000000000000",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
