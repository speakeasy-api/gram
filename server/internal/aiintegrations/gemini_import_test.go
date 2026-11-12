package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	vault "github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
	"github.com/stretchr/testify/require"
)

type geminiFakeClient struct {
	// export is what GetExport returns, and what CreateExport returns unless
	// created is set.
	export      *vault.Export
	created     *vault.Export
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
	findNames   []string
	// findOnce returns find for the first recorded lookup only.
	findOnce bool
}

func (c *geminiFakeClient) FindExport(_ context.Context, name string) (*vault.Export, error) {
	c.findNames = append(c.findNames, name)
	if c.findOnce && len(c.findNames) > 1 {
		return nil, nil
	}
	return c.find, nil
}
func (c *geminiFakeClient) CreateExport(_ context.Context, name string, start, end time.Time) (*vault.Export, error) {
	c.creates++
	c.name = name
	c.start = start
	c.end = end
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.created != nil {
		return c.created, nil
	}
	return c.export, nil
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

func geminiTestCursor(t *testing.T, cursor geminiExportCursor) string {
	t.Helper()
	encoded, err := encodeGeminiResult(cursor, false)
	require.NoError(t, err)
	return encoded.Cursor
}

func decodeGeminiTestCursor(t *testing.T, raw string) geminiExportCursor {
	t.Helper()
	cursor, err := decodeGeminiCursor(raw, time.Time{})
	require.NoError(t, err)
	return cursor
}

func completedGeminiExport(id string) *vault.Export {
	return &vault.Export{ID: id, Status: "COMPLETED", Files: []vault.File{{ObjectName: "owner.zip"}, {ObjectName: "counts.csv"}, {ObjectName: "errors.xml"}}}
}

func TestGeminiFirstSyncExportsLookbackThroughNowInOneExport(t *testing.T) {
	t.Parallel()
	saved := time.Date(2026, 11, 12, 17, 6, 0, 0, time.UTC)
	now := saved.Add(9 * time.Minute)
	cfg := geminiTestConfig()
	cfg.CreatedAt = saved.Add(-time.Hour)
	cfg.UpdatedAt = saved
	client := &geminiFakeClient{export: &vault.Export{ID: "export-1", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error {
		t.Fatal("must not import pending export")
		return nil
	})
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Equal(t, 1, client.creates)
	require.Equal(t, saved.Add(-geminiInitialLookback), client.start, "30 days before the save that started the sync")
	require.Equal(t, now, client.end, "one export reaches the poll time")
	require.Equal(t, client.start, result.Watermark, "a pending export must not advance the watermark")
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "export-1", cursor.ExportID)
	require.True(t, cursor.Live)

	cfg.LastCursor = result.Cursor
	again, err := svc.SyncGemini(t.Context(), cfg, now.Add(geminiPendingRecheck))
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, 1, client.gets)
	require.Equal(t, 1, client.creates, "resume must not create another job")
}

func TestGeminiSyncLookbackStartsFromLatestSave(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		updated time.Time
		anchor  time.Time
	}{
		{"key or unit changed long after connecting", created.Add(90 * 24 * time.Hour), created.Add(90 * 24 * time.Hour)},
		{"no update time", time.Time{}, created},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := geminiTestConfig()
			cfg.CreatedAt = created
			cfg.UpdatedAt = tc.updated
			client := &geminiFakeClient{export: &vault.Export{ID: "export-1", Status: "IN_PROGRESS"}}
			_, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, tc.anchor.Add(time.Minute))
			require.NoError(t, err)
			require.Equal(t, tc.anchor.Add(-geminiInitialLookback), client.start)
		})
	}
}

func TestGeminiSyncImportsCompletedExportAndStopsWhenCaughtUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	start := now.Add(-geminiInitialLookback)
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: start, Sequence: 1, ExportID: "export-1", Start: start, End: now.Add(-5 * time.Minute), Live: true}
	cfg.LastCursor = geminiTestCursor(t, pending)
	client := &geminiFakeClient{export: completedGeminiExport("export-1")}
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
	require.False(t, result.Pending, "a caught-up sync waits for the hourly poll")
	require.Equal(t, pending.End, result.Watermark)
	done := decodeGeminiTestCursor(t, result.Cursor)
	require.Empty(t, done.ExportID)
	require.True(t, done.End.IsZero())
	require.True(t, done.Overlap, "the next export re-checks late-indexed conversations")
	require.Equal(t, uint64(1), done.Sequence)
	require.Zero(t, client.creates)
	require.Zero(t, client.deletes, "retain successful artifacts until durable checkpoint")

	svc.importFiles = func(context.Context, Config, []geminiExportFile) error { return errors.New("write failed") }
	result, err = svc.SyncGemini(t.Context(), cfg, now)
	require.ErrorContains(t, err, "write failed")
	require.Equal(t, GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: pending.Through, Pending: false}, result)
}

func TestGeminiSyncNextExportOverlapsAndEndsNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 19, 5, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	done := geminiExportCursor{Version: geminiCursorVersion, Through: now.Add(-time.Hour), Sequence: 4, Overlap: true}
	cfg.LastCursor = geminiTestCursor(t, done)
	client := &geminiFakeClient{export: &vault.Export{ID: "refresh", Status: "IN_PROGRESS"}}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Equal(t, done.Through.Add(-geminiExportOverlap), client.start)
	require.Equal(t, now, client.end)
	require.Equal(t, done.Through, result.Watermark)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, uint64(5), cursor.Sequence)
	previous := done
	previous.Start = client.start
	require.NotEqual(t, geminiExportName(cfg.ID, previous), client.name, "each export gets a new name")
}

func TestGeminiSyncDropsOverlapBeforeSplittingWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	through := now.Add(-2 * time.Hour)
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: through, Sequence: 5, Overlap: true, ExportID: "big", Start: through.Add(-geminiExportOverlap), End: now.Add(-time.Minute), Live: true}
	cfg.LastCursor = geminiTestCursor(t, pending)
	big := completedGeminiExport("big")
	big.SizeBytes = geminiMaxExportBytes + 1
	client := &geminiFakeClient{export: big, created: &vault.Export{ID: "narrower", Status: "IN_PROGRESS"}}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Zero(t, client.downloads, "the reported size is checked before download")
	require.Equal(t, 1, client.deletes)
	require.Equal(t, through, client.start, "the overlap re-check goes first")
	require.Equal(t, pending.End, client.end)
	require.Equal(t, through, result.Watermark)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "narrower", cursor.ExportID)
	require.True(t, cursor.Live)
}

func TestGeminiSyncHalvesTooLargeWindowThenCatchesUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	start := now.Add(-geminiInitialLookback)
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: start, Sequence: 1, ExportID: "big", Start: start, End: now.Add(-10 * time.Minute), Live: true}
	cfg.LastCursor = geminiTestCursor(t, pending)
	client := &geminiFakeClient{export: completedGeminiExport("big"), created: &vault.Export{ID: "half", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error {
		return fmt.Errorf("parse: %w", errGeminiExportTooLarge)
	})
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Equal(t, 1, client.deletes)
	half := start.Add((pending.End.Sub(start) / 2).Truncate(time.Second))
	require.Equal(t, start, client.start)
	require.Equal(t, half, client.end)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "half", cursor.ExportID)
	require.False(t, cursor.Live)
	require.Equal(t, start, result.Watermark)

	// The first half imports, and the rest starts at once without overlap.
	later := now.Add(geminiPendingRecheck)
	cfg.LastCursor = result.Cursor
	client.export = completedGeminiExport("half")
	client.created = &vault.Export{ID: "rest", Status: "IN_PROGRESS"}
	svc.importFiles = func(context.Context, Config, []geminiExportFile) error { return nil }
	result, err = svc.SyncGemini(t.Context(), cfg, later)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Equal(t, half, result.Watermark)
	require.Equal(t, half, client.start)
	require.Equal(t, later, client.end)
	cursor = decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "rest", cursor.ExportID)
	require.False(t, cursor.Overlap)
	require.True(t, cursor.Live)
}

func TestGeminiSyncFailsWhenOneHourWindowIsTooLarge(t *testing.T) {
	t.Parallel()
	through := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: through, Sequence: 3, ExportID: "big", Start: through, End: through.Add(geminiMinExportWindow)}
	cfg.LastCursor = geminiTestCursor(t, pending)
	big := completedGeminiExport("big")
	big.SizeBytes = geminiMaxExportBytes + 1
	client := &geminiFakeClient{export: big}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, through.Add(2*time.Hour))
	require.ErrorContains(t, err, "even for one hour")
	require.Equal(t, GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: through, Pending: false}, result)
	require.Zero(t, client.creates)
}

func TestGeminiSyncKeepsProgressWhenNextExportCannotStart(t *testing.T) {
	t.Parallel()
	through := time.Date(2026, 10, 13, 17, 0, 0, 0, time.UTC)
	now := through.Add(30 * 24 * time.Hour)
	cfg := geminiTestConfig()
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: through, Sequence: 2, ExportID: "half", Start: through, End: through.Add(15 * 24 * time.Hour)}
	cfg.LastCursor = geminiTestCursor(t, pending)
	client := &geminiFakeClient{export: completedGeminiExport("half"), createErr: errors.New("vault unavailable")}
	svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error { return nil })
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err, "the import is kept; the next run starts the export")
	require.True(t, result.Pending)
	require.Equal(t, pending.End, result.Watermark)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Empty(t, cursor.ExportID)
	require.Equal(t, pending.End, cursor.Start)
	require.Equal(t, now, cursor.End)

	cfg.LastCursor = result.Cursor
	client.createErr = nil
	client.created = &vault.Export{ID: "rest", Status: "IN_PROGRESS"}
	result, err = svc.SyncGemini(t.Context(), cfg, now.Add(geminiPendingRecheck))
	require.NoError(t, err)
	require.Equal(t, pending.End, client.start)
	require.Equal(t, now, client.end, "the saved window is started as chosen")
	require.Equal(t, "rest", decodeGeminiTestCursor(t, result.Cursor).ExportID)
}

func TestGeminiSyncContinuesVersionOneDailyCursorThroughNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	day := now.Truncate(24 * time.Hour).Add(-28 * 24 * time.Hour)
	t.Run("pending daily export", func(t *testing.T) {
		t.Parallel()
		cfg := geminiTestConfig()
		cfg.LastCursor = geminiTestCursor(t, geminiExportCursor{Version: 1, Through: day.Add(-24 * time.Hour), Sequence: 1, ExportID: "daily", Start: day.Add(-72 * time.Hour), End: day})
		client := &geminiFakeClient{export: completedGeminiExport("daily"), created: &vault.Export{ID: "catch-up", Status: "IN_PROGRESS"}}
		result, err := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error { return nil }).SyncGemini(t.Context(), cfg, now)
		require.NoError(t, err)
		require.True(t, result.Pending)
		require.Equal(t, day, result.Watermark)
		require.Equal(t, day, client.start)
		require.Equal(t, now, client.end)
		cursor := decodeGeminiTestCursor(t, result.Cursor)
		require.Equal(t, geminiCursorVersion, cursor.Version)
		require.Equal(t, "catch-up", cursor.ExportID)
	})
	t.Run("between daily exports", func(t *testing.T) {
		t.Parallel()
		cfg := geminiTestConfig()
		cfg.LastCursor = geminiTestCursor(t, geminiExportCursor{Version: 1, Through: day, Sequence: 3})
		client := &geminiFakeClient{export: &vault.Export{ID: "catch-up", Status: "IN_PROGRESS"}}
		_, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
		require.NoError(t, err)
		require.Equal(t, day, client.start)
		require.Equal(t, now, client.end)
	})
}

func TestGeminiSyncRecoversExpiredExportForSameWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: now.Add(-24 * time.Hour), Sequence: 2, Overlap: true, ExportID: "expired", Start: now.Add(-72 * time.Hour), End: now.Add(-23 * time.Hour), Live: true}
	cfg.LastCursor = geminiTestCursor(t, pending)
	client := &geminiFakeClient{export: &vault.Export{ID: "replacement", Status: "IN_PROGRESS"}, getErr: &vault.HTTPError{StatusCode: http.StatusNotFound}}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now.Add(48*time.Hour))
	require.NoError(t, err)
	require.Equal(t, pending.Start, client.start)
	require.Equal(t, pending.End, client.end)
	require.Equal(t, pending.Through, result.Watermark)
	require.Equal(t, 1, client.creates)
	require.Equal(t, "replacement", decodeGeminiTestCursor(t, result.Cursor).ExportID)
}

func TestGeminiSyncFindsJobAfterLostCreateResponse(t *testing.T) {
	t.Parallel()
	saved := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	cfg.UpdatedAt = saved
	firstEnd := saved.Add(5 * time.Minute)
	client := &geminiFakeClient{find: &vault.Export{ID: "existing", Status: "IN_PROGRESS", Start: saved.Add(-geminiInitialLookback), End: firstEnd}}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, saved.Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, client.creates)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "existing", cursor.ExportID)
	require.Equal(t, firstEnd, cursor.End, "use the window Vault exported, not the retry time")
}

func TestGeminiSyncRetryUsesSameExportName(t *testing.T) {
	t.Parallel()
	saved := time.Date(2026, 10, 20, 23, 59, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	cfg.UpdatedAt = saved
	client := &geminiFakeClient{export: &vault.Export{ID: "pending", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, nil)
	_, err := svc.SyncGemini(t.Context(), cfg, saved)
	require.NoError(t, err)
	name, start, end := client.name, client.start, client.end
	// The create response was lost and the cursor was not saved, so the retry
	// starts from the same progress and must find the job instead of creating one.
	client.find = &vault.Export{ID: "pending", Status: "IN_PROGRESS", Start: start, End: end}
	result, err := svc.SyncGemini(t.Context(), cfg, saved.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, client.creates)
	require.Equal(t, []string{name, name}, client.findNames)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "pending", cursor.ExportID)
	require.Equal(t, start, cursor.Start)
	require.Equal(t, end, cursor.End, "the retry keeps the window of the job it found")
}

func TestGeminiSyncReplacesFoundJobWithoutTimeWindow(t *testing.T) {
	t.Parallel()
	saved := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	now := saved.Add(2 * time.Hour)
	cfg := geminiTestConfig()
	cfg.UpdatedAt = saved
	client := &geminiFakeClient{find: &vault.Export{ID: "unbounded", Status: "IN_PROGRESS"}, created: &vault.Export{ID: "replacement", Status: "IN_PROGRESS"}}
	svc := geminiTestImporter(client, nil)
	// The fake finds the unbounded job under every name. A second unbounded
	// job stops the sync instead of looping.
	_, err := svc.SyncGemini(t.Context(), cfg, now)
	require.ErrorContains(t, err, "does not report its time window")
	require.Equal(t, 1, client.deletes)
	require.Zero(t, client.creates)

	client.deletes, client.findNames = 0, nil
	client.findOnce = true
	result, err := svc.SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.Equal(t, 1, client.deletes, "the job with no window is removed")
	require.Equal(t, 1, client.creates)
	require.Len(t, client.findNames, 2)
	require.NotEqual(t, client.findNames[0], client.findNames[1], "the replacement uses the next name")
	require.Equal(t, client.findNames[1], client.name)
	require.Equal(t, now, client.end)
	cursor := decodeGeminiTestCursor(t, result.Cursor)
	require.Equal(t, "replacement", cursor.ExportID)
	require.Equal(t, uint64(2), cursor.Sequence)
}

func TestGeminiSyncSplitsWhenDownloadIsOverLimit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	start := now.Add(-geminiInitialLookback)
	pending := geminiExportCursor{Version: geminiCursorVersion, Through: start, Sequence: 1, ExportID: "big", Start: start, End: now.Add(-10 * time.Minute), Live: true}
	cfg.LastCursor = geminiTestCursor(t, pending)
	client := &geminiFakeClient{export: completedGeminiExport("big"), downloadErr: fmt.Errorf("download: %w", vault.ErrTooLarge), created: &vault.Export{ID: "half", Status: "IN_PROGRESS"}}
	result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
	require.NoError(t, err)
	require.True(t, result.Pending)
	require.Equal(t, 1, client.downloads, "stop at the first file over the bound")
	require.Equal(t, 1, client.deletes)
	require.Equal(t, start, client.start)
	require.True(t, client.end.Before(pending.End))
	require.Equal(t, "half", decodeGeminiTestCursor(t, result.Cursor).ExportID)
}

func TestGeminiShrinkNeverGoesBelowMinimumWindow(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cursor := geminiExportCursor{Version: geminiCursorVersion, Through: start, Sequence: 1, ExportID: "big", Start: start, End: start.Add(90 * time.Minute)}
	next, ok := cursor.shrink()
	require.True(t, ok)
	require.Equal(t, start.Add(geminiMinExportWindow), next.End, "a 90-minute window splits to one hour, not 45 minutes")
	_, ok = next.shrink()
	require.False(t, ok, "a one-hour window that is still too large stops splitting")
}

func TestGeminiSyncFailedOrIncompleteExportNeverAdvances(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	for _, status := range []string{"FAILED", "COMPLETED", "EXPORT_STATUS_UNSPECIFIED"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			cfg := geminiTestConfig()
			cursor := geminiExportCursor{Version: geminiCursorVersion, Through: now.Add(-24 * time.Hour), Sequence: 2, ExportID: "failed", Start: now.Add(-24 * time.Hour), End: now.Add(-time.Hour), Live: true}
			cfg.LastCursor = geminiTestCursor(t, cursor)
			client := &geminiFakeClient{export: &vault.Export{ID: "failed", Status: status}}
			result, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
			require.Error(t, err)
			require.Equal(t, GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: cursor.Through, Pending: false}, result)
			if status == "FAILED" {
				require.Equal(t, 1, client.deletes)
			}
		})
	}
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
			cursor := geminiExportCursor{Version: geminiCursorVersion, Through: now.Add(-24 * time.Hour), Sequence: 2, ExportID: "partial", Start: now.Add(-24 * time.Hour), End: now.Add(-time.Hour), Live: true}
			cfg.LastCursor = geminiTestCursor(t, cursor)
			client := &geminiFakeClient{export: &vault.Export{ID: "partial", Status: "COMPLETED", Files: []vault.File{{ObjectName: "export.zip"}}}}
			svc := geminiTestImporter(client, func(context.Context, Config, []geminiExportFile) error { return tc.failure })
			result, err := svc.SyncGemini(t.Context(), cfg, now)
			require.ErrorIs(t, err, tc.failure)
			require.Equal(t, GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: cursor.Through, Pending: false}, result)
			require.Equal(t, tc.deletes, client.deletes)
		})
	}
}

func TestGeminiSyncRejectsCursorAheadOfNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	cfg := geminiTestConfig()
	cfg.LastCursor = geminiTestCursor(t, geminiExportCursor{Version: geminiCursorVersion, Through: now.Add(time.Hour), Sequence: 1})
	client := &geminiFakeClient{}
	_, err := geminiTestImporter(client, nil).SyncGemini(t.Context(), cfg, now)
	require.ErrorContains(t, err, "ahead of the current time")
	require.Zero(t, client.creates)
}

func TestGeminiCursorRejectsMalformedOrUnboundedWindows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{
		`not json`,
		`{"version":3,"through":"2026-10-19T00:00:00Z"}`,
		`{"version":2}`,
		`{"version":1,"through":"2026-10-19T12:00:00Z"}`,
		`{"version":1,"through":"2026-10-19T00:00:00Z","export_id":"pending"}`,
		`{"version":1,"through":"2026-10-19T00:00:00Z","export_id":"pending","start":"2020-01-01T00:00:00Z","end":"2026-10-20T00:00:00Z"}`,
		`{"version":2,"through":"2026-10-19T12:00:00Z","export_id":"pending"}`,
		`{"version":2,"through":"2026-10-19T12:00:00Z","start":"2020-01-01T00:00:00Z","end":"2026-10-20T00:00:00Z"}`,
		`{"version":2,"through":"2026-10-19T12:00:00Z","start":"2026-10-19T12:00:00Z","end":"2026-10-19T12:00:00Z"}`,
	} {
		_, err := decodeGeminiCursor(raw, now)
		require.Error(t, err, raw)
	}
	cursor, err := decodeGeminiCursor(`{"version":2,"through":"2026-10-19T12:34:56Z","sequence":4,"overlap":true}`, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC), cursor.Through, "version 2 progress is not tied to UTC midnight")
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
