package unproxiedmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/unproxied_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateServer_StaffEmailSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	name := "Vendor Server"
	description := "A vendor MCP server we never proxy"
	server, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             &name,
		URL:              "https://vendor.example.com/mcp",
		Description:      &description,
	})
	require.NoError(t, err)
	require.NotEmpty(t, server.ID)
	require.Equal(t, "https://vendor.example.com/mcp", server.URL)
	require.NotNil(t, server.Name)
	require.Equal(t, name, *server.Name)
	require.NotNil(t, server.Description)
	require.Equal(t, description, *server.Description)
	require.NotNil(t, server.Slug)
}

func TestCreateServer_NonStaffEmailForbidden(t *testing.T) {
	t.Parallel()

	// newTestService seeds the default mock user (dev@example.com), which is
	// not a Speakeasy-owned domain, so CreateServer must reject it.
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://vendor.example.com/mcp",
		Description:      nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateServer_ListAndGet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://vendor.example.com/mcp",
		Description:      nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.UnproxiedMcpServers, 1)
	require.Equal(t, created.ID, listed.UnproxiedMcpServers[0].ID)

	fetched, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)

	err = ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Clients reach unproxied servers directly, so hosts that only resolve or
// route inside the client's network must be accepted.
func TestCreateServer_AcceptsPrivateNetworkHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "tailnet name", url: "https://mcp.tailnet-0000.ts.net/mcp"},
		{name: "unresolvable name", url: "https://mcp.internal.invalid/mcp"},
		{name: "shared address space ip", url: "http://100.64.0.10:8080/mcp"},
		{name: "private ip", url: "http://10.0.0.1/mcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			ctx = withStaffEmail(t, ctx)

			server, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
				SessionToken:     nil,
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
				Name:             nil,
				URL:              tc.url,
				Description:      nil,
			})
			require.NoError(t, err)
			require.Equal(t, tc.url, server.URL)
		})
	}
}

func TestCreateServer_RejectsMissingHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https:///mcp",
		Description:      nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServer_RejectsCredentialsInURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://user:secret@vendor.example.com/mcp",
		Description:      nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateServer_RejectsInvalidScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withStaffEmail(t, ctx)

	_, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "ftp://vendor.example.com/mcp",
		Description:      nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteServer_RejectsNonStaff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	staffCtx := withStaffEmail(t, ctx)

	created, err := ti.service.CreateServer(staffCtx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             nil,
		URL:              "https://vendor.example.com/mcp",
		Description:      nil,
	})
	require.NoError(t, err)

	// withStaffEmail mutates the shared AuthContext in place (both ctx and
	// staffCtx point at the same struct), so restore the original non-staff
	// email before exercising the delete-as-non-staff path below.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	nonStaffEmail := "dev@example.com"
	authCtx.Email = &nonStaffEmail

	err = ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
