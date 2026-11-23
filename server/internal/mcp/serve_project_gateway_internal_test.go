package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestProjectGatewayClientTokenProjectIDs(t *testing.T) {
	t.Parallel()
	project, other := uuid.New(), uuid.New()
	service := &Service{}
	endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.OrganizationGateway, ProjectID: project}
	client := remotesessions.Client{ProjectID: uuid.NullUUID{UUID: other, Valid: true}}
	projects, err := service.clientTokenProjectIDs(t.Context(), endpoint, client)
	require.NoError(t, err)
	require.Empty(t, projects, "a project gateway must not resolve another project's credentials")
	client.ProjectID.UUID = project
	projects, err = service.clientTokenProjectIDs(t.Context(), endpoint, client)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{project}, projects)
	endpoint.ProjectID = uuid.Nil
	client.ProjectID.UUID = other
	projects, err = service.clientTokenProjectIDs(t.Context(), endpoint, client)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{other}, projects, "organization gateways retain cross-project credentials")
}

func TestProjectGatewayRequestIdentity(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("POST", "/mcp/gateway/acme/projects/example", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", "acme")
	route.URLParams.Add("projectSlug", "example")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	require.Equal(t, "acme/projects/example", organizationGatewayRequestSlug(r))
}
