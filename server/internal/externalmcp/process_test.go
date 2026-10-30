package externalmcp

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/stretchr/testify/require"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestToolExtractorRetiredPulse(t *testing.T) {
	var remoteCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { remoteCalls.Add(1) }))
	defer remote.Close()
	// A closed, lazily created pool permits event flush failure without any DB connection.
	db, err := pgxpool.New(t.Context(), "postgres://localhost/unused")
	require.NoError(t, err)
	db.Close()
	native := &failedCatalogReader{}
	catalog := NewCatalogService(nil, nil, native)
	registryID := uuid.New()
	catalog.repo = routingRepo{rows: []repo.ListMCPRegistriesRow{{ID: registryID, Url: "https://api.pulsemcp.com"}}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	task := ToolExtractorTask{ProjectID: uuid.New(), DeploymentID: uuid.New(), OrgSlug: "test-org", ProjectSlug: "test-project", MCP: ToolExtractorTaskMCPServer{AttachmentID: uuid.New(), RegistryID: uuid.NullUUID{UUID: registryID, Valid: true}, Name: "retired", Slug: "retired", RegistryServerSpecifier: "example/server", SelectedRemotes: []string{remote.URL}}}
	err = NewToolExtractor(logger, nil, db, catalog).Do(t.Context(), task)
	require.ErrorIs(t, err, ErrCatalogSourceRetired)
	require.Zero(t, native.calls, "must not fall back")
	require.Zero(t, remoteCalls.Load(), "remote discovery must not start")
	found := false
	for line := range bytes.SplitSeq(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		if record["gram.external_mcp.failure_reason"] != "retired_pulse_catalog" {
			continue
		}
		found = true
		require.Equal(t, "ERROR", record["level"])
		require.Equal(t, task.DeploymentID.String(), record[string(attr.DeploymentIDKey)])
		require.Equal(t, task.MCP.AttachmentID.String(), record[string(attr.ExternalMCPIDKey)])
		require.Equal(t, task.ProjectID.String(), record[string(attr.ProjectIDKey)])
		require.Equal(t, registryID.String(), record[string(attr.MCPRegistryIDKey)])
	}
	require.True(t, found, "missing stable retired-source failure log")
}
