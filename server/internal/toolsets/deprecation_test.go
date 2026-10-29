package toolsets_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ := b.buf.Write(p)
	return n, nil
}

func (b *lockedBuffer) deprecationRecords(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["event"] == "toolsets:deprecated-mcp-hosting-api" {
			records = append(records, record)
		}
	}
	b.buf.Reset()
	return records
}

func TestToolsetsService_DeprecatedHostingInputsLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	buf := &lockedBuffer{}
	ti.service.SetLoggerForTest(slog.New(slog.NewJSONHandler(buf, nil)))

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Deprecation Log Toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, buf.deprecationRecords(t))

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, Name: new("Renamed")})
	require.NoError(t, err)
	require.Empty(t, buf.deprecationRecords(t))

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, McpIsPublic: new(false)})
	require.ErrorIs(t, err, toolsets.ErrHostingFieldsMoved, "a rejected request is still attributed")
	records := buf.deprecationRecords(t)
	require.Len(t, records, 1)
	require.Equal(t, []any{"mcp_is_public"}, records[0]["gram.deprecated.fields"])
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, authCtx.ActiveOrganizationID, records[0]["gram.org.id"])
	require.Equal(t, authCtx.UserID, records[0]["user.id"])
}
