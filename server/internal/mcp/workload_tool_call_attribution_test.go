package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A workload's tool call is recorded as its agent's, so the agent's tool usage
// counts it, and keeps the workload's issuer and subject so the call can be
// traced back to the workload that made it.
func TestServePublic_WorkloadToolCallIsRecordedAsItsAgent(t *testing.T) {
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

	requireTelemetryRowCount(t, `event_source = ?
		   AND toString(attributes.gram.authorization.actor.type) = ?
		   AND toString(attributes.gram.authorization.actor.id) = ?
		   AND toString(attributes.gram.workload_issuer.id) = ?
		   AND toString(attributes.gram.workload.subject) = ?`,
		1, string(telemetry.EventSourceToolCall), string(urn.PrincipalTypeAgent), fx.agent.ID.String(), issuerID.String(), subject)
}
