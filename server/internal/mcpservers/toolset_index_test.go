package mcpservers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// indexRequest is one call the service made to its toolset index trigger.
type indexRequest struct {
	projectID uuid.UUID
	toolsetID uuid.UUID
}

// recordingIndexTrigger stands in for the toolsets package's trigger. It
// records every request and answers each with err. The service calls it
// synchronously from the handler, so it needs no locking.
type recordingIndexTrigger struct {
	requests []indexRequest
	err      error
}

func (r *recordingIndexTrigger) trigger(_ context.Context, projectID, toolsetID uuid.UUID) error {
	r.requests = append(r.requests, indexRequest{projectID: projectID, toolsetID: toolsetID})
	return r.err
}

func (r *recordingIndexTrigger) taken() []indexRequest {
	requests := r.requests
	r.requests = nil
	return requests
}

func createServerOnToolset(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, visibility string) *types.McpServer {
	t.Helper()

	id := toolsetID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:       "index trigger server " + uuid.NewString()[:8],
		ToolsetID:  &id,
		Visibility: types.McpServerVisibility(visibility),
	})
	require.NoError(t, err)
	return created
}

func TestCreateMcpServer_RequestsIndexForTheToolsetItFronts(t *testing.T) {
	t.Parallel()

	recorder := &recordingIndexTrigger{}
	ctx, ti := newTestService(t)
	ti.service.WithToolsetIndexTrigger(recorder.trigger)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Not MCP-enabled: when its version was written nothing served it, so
	// only the server that now fronts it can request its index.
	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.False(t, toolset.McpEnabled)

	createServerOnToolset(t, ctx, ti, toolset.ID, "private")
	require.Equal(t, []indexRequest{{projectID: *authCtx.ProjectID, toolsetID: toolset.ID}}, recorder.taken(),
		"a new server fronting the toolset makes it served, so its index must be requested")

	other := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	createServerOnToolset(t, ctx, ti, other.ID, "disabled")
	require.Empty(t, recorder.taken(), "a disabled server serves nothing, so the toolset needs no index yet")
}

func TestUpdateMcpServer_RequestsIndexForTheToolsetItIsRepointedAt(t *testing.T) {
	t.Parallel()

	recorder := &recordingIndexTrigger{}
	ctx, ti := newTestService(t)
	ti.service.WithToolsetIndexTrigger(recorder.trigger)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	second := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	server := createServerOnToolset(t, ctx, ti, first.ID, "private")
	recorder.taken()

	secondID := second.ID.String()
	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:         server.ID,
		ToolsetID:  &secondID,
		Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, []indexRequest{{projectID: *authCtx.ProjectID, toolsetID: second.ID}}, recorder.taken(),
		"pointing the server at another toolset makes that toolset served")

	renamed := "renamed index trigger server"
	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:         server.ID,
		Name:       &renamed,
		ToolsetID:  &secondID,
		Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Empty(t, recorder.taken(), "an update that leaves the server fronting the same toolset changes nothing about what is served")
}

func TestUpdateMcpServer_RequestsIndexWhenADisabledServerIsEnabled(t *testing.T) {
	t.Parallel()

	recorder := &recordingIndexTrigger{}
	ctx, ti := newTestService(t)
	ti.service.WithToolsetIndexTrigger(recorder.trigger)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	server := createServerOnToolset(t, ctx, ti, toolset.ID, "disabled")
	require.Empty(t, recorder.taken())

	toolsetID := toolset.ID.String()
	_, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:         server.ID,
		ToolsetID:  &toolsetID,
		Visibility: types.McpServerVisibility("private"),
	})
	require.NoError(t, err)
	require.Equal(t, []indexRequest{{projectID: *authCtx.ProjectID, toolsetID: toolset.ID}}, recorder.taken(),
		"enabling the server is what makes the toolset served")
}

func TestCreateMcpServer_IndexTriggerFailureDoesNotFailTheCreate(t *testing.T) {
	t.Parallel()

	recorder := &recordingIndexTrigger{err: errors.New("temporal unavailable")}
	ctx, ti := newTestService(t)
	ti.service.WithToolsetIndexTrigger(recorder.trigger)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	created := createServerOnToolset(t, ctx, ti, toolset.ID, "private")
	require.Len(t, recorder.taken(), 1, "the trigger ran and failed")

	// The server committed before the trigger ran, and the periodic sweep
	// still reaches the toolset, so the failure must not undo or misreport it.
	stored, err := repo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, toolset.ID, stored.ToolsetID.UUID)
}
