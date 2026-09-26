package remotemcp_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestCallerAssertionHeadersCannotBeConfigured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	existing := createSecretHeader(t, ctx, ti, server.ID, "X-Upstream-Key", "test-secret")
	for _, reserved := range []string{"X-Speakeasy-Identity", "X_speakeasy_identity", "x-speakeasy-identity", "X_Speakeasy-Identity"} {
		_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, reserved, func(p *gen.CreateServerHeaderPayload) { p.Value = new("forged") }))
		require.Error(t, err)
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Copied", func(p *gen.CreateServerHeaderPayload) { p.ValueFromRequestHeader = &reserved }))
		require.Error(t, err)
		requireOopsCode(t, err, oops.CodeBadRequest)
		// Omitting the existing secret value must not bypass name validation.
		_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(existing.ID, reserved, func(p *gen.UpdateServerHeaderPayload) { p.IsSecret = new(true) }))
		require.Error(t, err)
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(existing.ID, "X-Copied", func(p *gen.UpdateServerHeaderPayload) { p.ValueFromRequestHeader = &reserved }))
		require.Error(t, err)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
