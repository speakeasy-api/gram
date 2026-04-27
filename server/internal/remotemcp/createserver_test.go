package remotemcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestCreateServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)

	payload := &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:        "X-API-Key",
				Description: new("API key for authentication"),
				IsRequired:  new(true),
				IsSecret:    new(true),
				Value:       new("secret-key-123"),
			},
			{
				Name:                   "X-Request-ID",
				Description:            new("Pass-through request ID"),
				IsRequired:             new(false),
				IsSecret:               new(false),
				ValueFromRequestHeader: new("X-Request-ID"),
			},
		},
	}

	result, err := ti.service.CreateServer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotEmpty(t, result.ID)
	require.NotEmpty(t, result.ProjectID)
	require.Equal(t, "https://mcp.example.com", result.URL)
	require.Equal(t, "streamable-http", result.TransportType)
	require.Len(t, result.Headers, 2)

	// Find the secret header and verify redaction
	for _, h := range result.Headers {
		if h.Name == "X-API-Key" {
			require.True(t, h.IsSecret)
			require.True(t, h.IsRequired)
			require.NotNil(t, h.Value)
			require.Contains(t, *h.Value, "*")
			require.Nil(t, h.ValueFromRequestHeader)
		}
		if h.Name == "X-Request-ID" {
			require.False(t, h.IsSecret)
			require.False(t, h.IsRequired)
			require.Nil(t, h.Value)
			require.NotNil(t, h.ValueFromRequestHeader)
			require.Equal(t, "X-Request-ID", *h.ValueFromRequestHeader)
		}
	}

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateServer_NoHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "https://mcp.example.com", result.URL)
	require.Equal(t, "streamable-http", result.TransportType)
	require.Empty(t, result.Headers)
}

func TestCreateServer_InvalidHeaderBothValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:                   "Bad-Header",
				Value:                  new("static-value"),
				ValueFromRequestHeader: new("X-Original"),
			},
		},
	}

	_, err := ti.service.CreateServer(ctx, payload)
	require.Error(t, err)
}

func TestCreateServer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Resource: authCtx.ProjectID.String(),
	})

	payload := &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	}

	_, err := ti.service.CreateServer(ctx, payload)
	require.Error(t, err)
}
