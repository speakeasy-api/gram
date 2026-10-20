package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A workload's tool call stays attributed to the workload as actor and names
// the agent it acted under, so the tool log can show the agent.
func TestServePublic_WorkloadToolCallRecordsItsAgent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadChallengeFixture(t, ctx, ti)
	addHTTPTools(t, ctx, ti, fx.toolset.ID, fx.target.ProjectID, fx.orgID, "deploy")
	slug := fx.toolset.McpSlug.String

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)
	headers := map[string]string{"Mcp-Test-Server-Url": upstream.URL}

	for _, body := range [][]byte{makeInitializeBody(), makeToolsCallBody("deploy")} {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, body, fx.token, headers)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		require.NotContains(t, decodeRPCResponse(t, w), "error")
	}

	issuerID, subject, err := fx.session.SubjectUrn.Workload()
	require.NoError(t, err)
	workload := urn.NewWorkloadPrincipal(issuerID, subject)

	requireTelemetryRowCount(t, `event_source = ?
		   AND toString(attributes.gram.authorization.actor.type) = ?
		   AND toString(attributes.gram.authorization.actor.id) = ?
		   AND toString(attributes.gram.authorization.agent.id) = ?`,
		1, string(telemetry.EventSourceToolCall), string(urn.PrincipalTypeWorkload), workload.ID, fx.agent.ID.String())
}
