package toolsets

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestDeprecatedHostingFields(t *testing.T) {
	t.Parallel()

	name := "renamed"
	require.Empty(t, deprecatedHostingFields(&gen.UpdateToolsetPayload{Slug: "toolset", Name: &name}))

	enabled := true
	public := false
	slug := types.Slug("")
	mode := types.NetworkAccessMode("public_only")
	domain := "00000000-0000-0000-0000-000000000001"
	require.Equal(t,
		[]string{"mcp_enabled", "mcp_slug", "mcp_is_public", "network_access_mode", "custom_domain_id"},
		deprecatedHostingFields(&gen.UpdateToolsetPayload{
			Slug:              "toolset",
			McpEnabled:        &enabled,
			McpSlug:           &slug,
			McpIsPublic:       &public,
			NetworkAccessMode: &mode,
			CustomDomainID:    &domain,
		}),
	)
}

func TestLogDeprecatedHostingUse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := contextvalues.SetRequestContext(context.Background(), &contextvalues.RequestContext{UserAgent: "external-client/1.0", RefererHost: "app.example.com"})
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "org-test", UserID: "user-test", APIKeyID: "key-test"}

	logDeprecatedHostingUse(ctx, logger, authCtx, []string{"mcp_slug"})

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, "WARN", record["level"])
	require.Equal(t, "toolsets:deprecated-mcp-hosting-api", record["event"])
	require.Equal(t, "external-client/1.0", record["http.request.header.user_agent"])
	require.Equal(t, "app.example.com", record["http.request.referer_host"])
	require.Equal(t, []any{"mcp_slug"}, record["gram.deprecated.fields"])
	require.Equal(t, "org-test", record["gram.org.id"])
	require.Equal(t, "user-test", record["user.id"])
	require.Equal(t, "key-test", record["gram.api_key.id"])

	buf.Reset()
	logDeprecatedHostingUse(context.Background(), logger, &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, nil)
	record = map[string]any{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Empty(t, record["http.request.header.user_agent"])
	require.NotContains(t, record, "gram.deprecated.fields")
	require.NotContains(t, record, "gram.api_key.id")
}
