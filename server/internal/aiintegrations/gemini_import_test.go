package aiintegrations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	vault "github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
	"github.com/stretchr/testify/require"
)

type geminiFakeClient struct {
	export      *vault.Export
	find        *vault.Export
	getErr      error
	downloadErr error
	createErr   error
	downloads   int
	creates     int
	gets        int
	deletes     int
	start, end  time.Time
	name        string
}

func (c *geminiFakeClient) FindExport(context.Context, string) (*vault.Export, error) {
	return c.find, nil
}
func (c *geminiFakeClient) CreateExport(_ context.Context, name string, start, end time.Time) (*vault.Export, error) {
	c.creates++
	c.name = name
	c.start = start
	c.end = end
	return c.export, c.createErr
}
func (c *geminiFakeClient) GetExport(context.Context, string) (*vault.Export, error) {
	c.gets++
	return c.export, c.getErr
}
func (c *geminiFakeClient) Download(context.Context, vault.File) ([]byte, error) {
	c.downloads++
	return []byte("test artifact"), c.downloadErr
}
func (c *geminiFakeClient) DeleteExport(context.Context, string) error { c.deletes++; return nil }

func geminiTestConfig() Config {
	return Config{ID: uuid.New(), Provider: ProviderGeminiCompliance, ProjectID: uuid.New(), OrganizationID: "org_test", ExternalOrganizationID: new("00000000-0000-4000-8000-000000000001")}
}

func geminiTestImporter(client *geminiFakeClient, write func(context.Context, Config, []geminiExportFile) error) *GeminiImportService {
	return &GeminiImportService{newClient: func(Config) (geminiVaultClient, error) { return client, nil }, importFiles: write}
}

func TestGeminiSyncPersistsPendingExportWithoutAdvancingWatermark(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	client := &geminiFakeClient{export: &vault.Export{ID: "export-1", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error {
		t.Fatal("must not import pending export")
		return nil
	})
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	cursor, err := decodeGeminiCursor(result.Cursor, now)
	require.NoError(t, err)
	require.Equal(t, "export-1", cursor.ExportID)
	require.Equal(t, now.UTC().Truncate(24*time.Hour).Add(-geminiInitialLookback), result.Watermark)
	require.Equal(t, cursor.Through.Add(-geminiExportOverlap), client.start)
	require.Equal(t, cursor.Through.Add(24*time.Hour), client.end)
	require.Equal(t, 1, client.creates)

	cfg.LastCursor = result.Cursor
	result2, err := svc.SyncGemini(t.Context(), cfg, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, result, result2)
	require.Equal(t, 1, client.gets)
	require.Equal(t, 1, client.creates, "resume must not create another job")
}

func TestGeminiSyncAdvancesOnlyAfterAllArtifactsAreImported(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	pending := geminiExportCursor{Version: 1, Through: now.Truncate(24 * time.Hour).Add(-24 * time.Hour), Sequence: 2, ExportID: "export-1", Start: now.Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour), End: now.Truncate(24 * time.Hour)}
	checkpoint, err := encodeGeminiResult(pending)
	require.NoError(t, err)
	cfg.LastCursor = checkpoint.Cursor
	client := &geminiFakeClient{export: &vault.Export{ID: "export-1", Status: "COMPLETED", Files: []vault.File{{ObjectName: "owner.zip"}, {ObjectName: "counts.csv"}, {ObjectName: "errors.xml"}}}}
	imported := false
	svc := geminiTestImporter(client, func(_ context.Context, _ Config, files []geminiExportFile) error {
		require.Equal(t, 3, client.downloads)
		require.Len(t, files, 3)
		imported = true
		return nil
	})
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, imported)
	require.Equal(t, pending.End, result.Watermark)
	done, err := decodeGeminiCursor(result.Cursor, now)
	require.NoError(t, err)
	require.Empty(t, done.ExportID)
	require.Equal(t, uint64(3), done.Sequence)
	require.Zero(t, client.deletes, "retain successful artifacts until durable checkpoint")

	imported = false
	svc.importFiles = func(context.Context, Config, []geminiExportFile) error { return errors.New("write failed") }
	result, err = svc.SyncGemini(t.Context(), cfg, now)
	require.ErrorContains(t, err, "write failed")
	require.Equal(t, checkpoint, result)
}

func TestGeminiSyncRecoversExpiredExportForSameWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	pending := geminiExportCursor{Version: 1, Through: now.Truncate(24 * time.Hour).Add(-24 * time.Hour), Sequence: 2, ExportID: "expired", Start: now.Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour), End: now.Truncate(24 * time.Hour)}
	checkpoint, err := encodeGeminiResult(pending)
	require.NoError(t, err)
	cfg.LastCursor = checkpoint.Cursor
	client := &geminiFakeClient{export: &vault.Export{ID: "replacement", Status: "IN_PROGRESS"}, getErr: &vault.HTTPError{StatusCode: http.StatusNotFound}}
	svc := geminiTestImporter(client, nil)
	result, err := svc.SyncGemini(t.Context(), cfg, now.Add(48*time.Hour))
	require.NoError(t, err)
	require.Equal(t, pending.Start, client.start)
	require.Equal(t, pending.End, client.end)
	require.Equal(t, pending.Through, result.Watermark)
	require.Equal(t, 1, client.creates)
}

func TestGeminiSyncFindsJobAfterLostCreateResponse(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	client := &geminiFakeClient{find: &vault.Export{ID: "existing", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, nil)
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.Zero(t, client.creates)
	cursor, err := decodeGeminiCursor(result.Cursor, now)
	require.NoError(t, err)
	require.Equal(t, "existing", cursor.ExportID)
}

func TestGeminiSyncCaughtUpRechecksHaveDistinctDeterministicNames(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	done := geminiExportCursor{Version: 1, Through: now.Truncate(24 * time.Hour), Sequence: 9}
	checkpoint, err := encodeGeminiResult(done)
	require.NoError(t, err)
	cfg.LastCursor = checkpoint.Cursor
	client := &geminiFakeClient{export: &vault.Export{ID: "refresh", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, nil)
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.Equal(t, done.Through.Add(-geminiExportOverlap), client.start)
	require.Equal(t, done.Through, client.end)
	require.Equal(t, done.Through, result.Watermark)
	next := done
	next.Sequence++
	require.NotEqual(t, geminiExportName(cfg.ID, done), geminiExportName(cfg.ID, next))
}

func TestGeminiSyncFailedOrIncompleteExportNeverAdvances(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	for _, status := range []string{"FAILED", "COMPLETED", "EXPORT_STATUS_UNSPECIFIED"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			cfg := geminiTestConfig()
			cursor := geminiExportCursor{Version: 1, Through: now.Truncate(24 * time.Hour).Add(-24 * time.Hour), Sequence: 2, ExportID: "failed", Start: now.Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour), End: now.Truncate(24 * time.Hour)}
			checkpoint, err := encodeGeminiResult(cursor)
			require.NoError(t, err)
			cfg.LastCursor = checkpoint.Cursor
			client := &geminiFakeClient{export: &vault.Export{ID: "failed", Status: status}}
			svc := geminiTestImporter(client, nil)
			result, err := svc.SyncGemini(t.Context(), cfg, now)
			require.Error(t, err)
			require.Equal(t, checkpoint, result)
			if status == "FAILED" {
				require.Equal(t, 1, client.deletes)
			}
		})
	}
}

func TestGeminiCursorRejectsMalformedOrUnboundedWindows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{`not json`, `{"version":2,"through":"2026-10-19T00:00:00Z"}`, `{"version":1,"through":"2026-10-19T12:00:00Z"}`, `{"version":1,"through":"2026-10-19T00:00:00Z","export_id":"pending"}`, `{"version":1,"through":"2026-10-19T00:00:00Z","export_id":"pending","start":"2020-01-01T00:00:00Z","end":"2026-10-20T00:00:00Z"}`} {
		_, err := decodeGeminiCursor(raw, now)
		require.Error(t, err)
	}
}

func TestGeminiMessageRowsPreserveRoleModelAndIdempotentIdentity(t *testing.T) {
	t.Parallel()
	cfg := geminiTestConfig()
	chatID := uuid.New()
	turn := geminiTurn{ID: "turn-1", OwnerEmail: "user@example.com", ConversationID: "conversation-1", Prompt: "hello\x00world", Response: "answer", Model: "gemini-test", Timestamp: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	rows := geminiMessageRows(cfg, chatID, "", turn)
	require.Len(t, rows, 2)
	require.Equal(t, "user", rows[0].Params.Role)
	require.Equal(t, "assistant", rows[1].Params.Role)
	require.Equal(t, "helloworld", rows[0].Params.Content)
	require.Equal(t, "gemini-test", rows[1].Params.Model.String)
	require.Equal(t, "gemini", rows[0].Params.Source.String)
	require.Equal(t, turn.Timestamp, rows[0].Params.CreatedAt.Time)
	require.NotEqual(t, rows[0].Params.ExternalMessageID, rows[1].Params.ExternalMessageID)
	require.Equal(t, rows, geminiMessageRows(cfg, chatID, "", turn))
	require.Zero(t, rows[1].Params.TotalTokens, "do not invent usage from transcripts")
	require.Equal(t, "google", rows[1].Provider)
	require.NotEqual(t, geminiExternalChatID("matter", "one@example.com", "same"), geminiExternalChatID("matter", "two@example.com", "same"))
	require.NotEqual(t, geminiExternalChatID("matter1", "one@example.com", "same"), geminiExternalChatID("matter2", "one@example.com", "same"))
}

func TestGeminiSyncPartialSourceExportIsRecreatedButLocalFailureIsRetained(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		failure error
		deletes int
	}{{"source partial", errGeminiPartialExport, 1}, {"local writer", errors.New("local failure"), 0}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := geminiTestConfig()
			cursor := geminiExportCursor{Version: 1, Through: now.Truncate(24 * time.Hour).Add(-24 * time.Hour), Sequence: 2, ExportID: "partial", Start: now.Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour), End: now.Truncate(24 * time.Hour)}
			checkpoint, err := encodeGeminiResult(cursor)
			require.NoError(t, err)
			cfg.LastCursor = checkpoint.Cursor
			client := &geminiFakeClient{export: &vault.Export{ID: "partial", Status: "COMPLETED", Files: []vault.File{{ObjectName: "export.zip"}}}}
			svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error { return tc.failure })
			result, err := svc.SyncGemini(t.Context(), cfg, now)
			require.ErrorIs(t, err, tc.failure)
			require.Equal(t, checkpoint, result)
			require.Equal(t, tc.deletes, client.deletes)
		})
	}
}

func TestGeminiSyncInitialWindowAnchoredAcrossMidnightRetry(t *testing.T) {
	t.Parallel()
	connectedAt := time.Date(2026, 10, 20, 23, 59, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	cfg.CreatedAt = connectedAt
	client := &geminiFakeClient{export: &vault.Export{ID: "pending", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, nil)
	_, err := svc.SyncGemini(t.Context(), cfg, connectedAt)
	require.NoError(t, err)
	name, start, end := client.name, client.start, client.end
	_, err = svc.SyncGemini(t.Context(), cfg, connectedAt.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, name, client.name)
	require.Equal(t, start, client.start)
	require.Equal(t, end, client.end)
}
