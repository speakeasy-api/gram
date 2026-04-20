package remotemcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateServer_ServerFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerUpdate)
	require.NoError(t, err)

	// Update server fields only, leave headers unchanged (nil)
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://mcp-v2.example.com"),
		TransportType:    new("sse"),
		Headers:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp-v2.example.com", updated.URL)
	require.Equal(t, "sse", updated.TransportType)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateServer_DesiredStateHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("original-secret"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Headers, 1)

	// Send desired state: update existing header + add a new one
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("updated-secret"),
			},
			{
				Name:                   "X-Trace-ID",
				IsSecret:               new(false),
				ValueFromRequestHeader: new("X-Trace-ID"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, updated.Headers, 2)
}

func TestUpdateServer_RemoveHeadersByOmission(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("some-secret"),
			},
			{
				Name:                   "X-Request-ID",
				ValueFromRequestHeader: new("X-Request-ID"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Headers, 2)

	// Send desired state with only one header — the other is removed
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("some-secret"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, updated.Headers, 1)
	require.Equal(t, "X-API-Key", updated.Headers[0].Name)
}

func TestUpdateServer_RemoveAllHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("secret"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Headers, 1)

	// Empty array means remove all headers
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)
	require.Empty(t, updated.Headers)
}

func TestUpdateServer_NilHeadersLeavesUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:                   "X-Request-ID",
				ValueFromRequestHeader: new("X-Request-ID"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Headers, 1)

	// nil headers = don't touch headers, only update URL
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://mcp-new.example.com"),
		Headers:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp-new.example.com", updated.URL)
	require.Len(t, updated.Headers, 1)
	require.Equal(t, "X-Request-ID", updated.Headers[0].Name)
}

func TestUpdateServer_PreserveExistingSecretValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("my-secret-value"),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Headers, 1)
	require.Equal(t, "***", *created.Headers[0].Value)

	// Omit value for existing secret header to preserve the stored value
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, updated.Headers, 1)
	require.Equal(t, "X-API-Key", updated.Headers[0].Name)
	require.True(t, updated.Headers[0].IsSecret)
	require.NotNil(t, updated.Headers[0].Value)
	require.Equal(t, "***", *updated.Headers[0].Value)
}

func TestUpdateServer_NewSecretHeaderWithoutValueReturnsError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)

	// Adding a new secret header without a value should return a 400, not a 500
	_, err = ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-New-Secret",
				IsSecret: new(true),
			},
		},
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateServer_PartialServerFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)

	// Only update URL, leave transport_type unchanged
	updated, err := ti.service.UpdateServer(ctx, &gen.UpdateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		URL:              new("https://mcp-new.example.com"),
		Headers:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp-new.example.com", updated.URL)
	require.Equal(t, "streamable-http", updated.TransportType)
}
