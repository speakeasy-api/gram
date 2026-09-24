package mcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type mcpBandwidthPublisher struct {
	mu       sync.Mutex
	messages []*meteringv1.MeterReading
}

func (p *mcpBandwidthPublisher) Publish(_ context.Context, message *meteringv1.MeterReading, _ ...gcp.PublishOption) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, message)
	return immediatePublishResult{}
}

func (p *mcpBandwidthPublisher) Stop(context.Context) error { return nil }

func (p *mcpBandwidthPublisher) snapshot() []*meteringv1.MeterReading {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*meteringv1.MeterReading(nil), p.messages...)
}

type immediatePublishResult struct{}

func (immediatePublishResult) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

func (immediatePublishResult) Get(context.Context) (string, error) { return "published", nil }

func TestMCPBandwidthAttributionForResolvedHostedEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "bandwidth-hosted-"+uuid.NewString()[:8])
	slug := "bandwidth-hosted-" + uuid.NewString()[:8]
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, uuid.Nil)
	body := makeInitializeBody()
	publisher := &mcpBandwidthPublisher{}
	handler := metering.NewMCPBandwidthMiddleware(ti.logger, publisher)(oops.MCPErrHandle(ti.logger, ti.service.ServePublic))
	recorder := httptest.NewRecorder()
	req := newMCPBandwidthRequest(ctx, "/mcp/"+slug, "mcpSlug", slug, body, "")

	handler.ServeHTTP(recorder, req)

	messages := publisher.snapshot()
	require.Len(t, messages, 2, recorder.Body.String())
	requireBandwidthMessage(t, messages[0], metering.MeterMCPBandwidthIngress, int64(len(body)), authCtx, "/mcp/"+slug, metering.MCPServerTypeHosted, mcpServer.ID.String(), mcpServer.Slug.String)
	requireBandwidthMessage(t, messages[1], metering.MeterMCPBandwidthEgress, int64(recorder.Body.Len()), authCtx, "/mcp/"+slug, metering.MCPServerTypeHosted, mcpServer.ID.String(), mcpServer.Slug.String)
}

func TestMCPBandwidthAttributionForAuthorizedPlatformToolset(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID := createAssistant(t, ti, authCtx, "Bandwidth managed assistant")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	}))
	token := mintAssistantToken(t, ti, authCtx, managedID)
	body := toolsListBody()
	slug := platformtools.ManagedAssistantPlatformToolsetSlug
	requestPath := "/platform/mcp/" + slug
	publisher := &mcpBandwidthPublisher{}
	handler := metering.NewMCPBandwidthMiddleware(ti.logger, publisher)(oops.MCPErrHandle(ti.logger, ti.service.ServePlatformToolset))
	recorder := httptest.NewRecorder()
	req := newMCPBandwidthRequest(t.Context(), requestPath, "toolsetSlug", slug, body, token)

	handler.ServeHTTP(recorder, req)

	messages := publisher.snapshot()
	require.Len(t, messages, 2, recorder.Body.String())
	requireBandwidthMessage(t, messages[0], metering.MeterMCPBandwidthIngress, int64(len(body)), authCtx, requestPath, metering.MCPServerTypePlatformToolset, slug, slug)
	requireBandwidthMessage(t, messages[1], metering.MeterMCPBandwidthEgress, int64(recorder.Body.Len()), authCtx, requestPath, metering.MCPServerTypePlatformToolset, slug, slug)
}

func TestMCPBandwidthDoesNotAttributeUnknownEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	slug := "unknown-" + uuid.NewString()
	publisher := &mcpBandwidthPublisher{}
	handler := metering.NewMCPBandwidthMiddleware(ti.logger, publisher)(oops.MCPErrHandle(ti.logger, ti.service.ServePublic))
	recorder := httptest.NewRecorder()
	req := newMCPBandwidthRequest(ctx, "/mcp/"+slug, "mcpSlug", slug, makeInitializeBody(), "")

	handler.ServeHTTP(recorder, req)

	require.Empty(t, publisher.snapshot())
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func newMCPBandwidthRequest(ctx context.Context, path, routeParam, routeValue string, body []byte, bearerToken string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(routeParam, routeValue)
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

func requireBandwidthMessage(t *testing.T, message *meteringv1.MeterReading, meterID metering.MeterID, value int64, authCtx *contextvalues.AuthContext, requestPath string, serverType metering.MCPServerType, serverID, serverSlug string) {
	t.Helper()
	require.Equal(t, string(meterID), message.GetMeterId())
	require.Equal(t, value, message.GetValue())
	require.Equal(t, authCtx.ActiveOrganizationID, message.GetOrganizationId())
	require.Equal(t, authCtx.ProjectID.String(), message.GetProjectId())
	require.Equal(t, string(metering.UnitBytes), message.GetUnit())
	require.Equal(t, string(metering.MeasurementHTTPBodyBytes), message.GetMeasurementMethod())
	require.Equal(t, requestPath, message.GetAttributes()[metering.AttributeRequestPath])
	require.Equal(t, string(serverType), message.GetAttributes()[metering.AttributeMCPServerType])
	require.Equal(t, serverID, message.GetAttributes()[metering.AttributeMCPServerID])
	require.Equal(t, serverSlug, message.GetAttributes()[metering.AttributeMCPServerSlug])
}

var _ gcp.Publisher[*meteringv1.MeterReading] = (*mcpBandwidthPublisher)(nil)
