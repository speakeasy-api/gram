package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/metering"
	vault "github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
)

const (
	geminiInitialLookback = 30 * 24 * time.Hour
	// geminiExportOverlap re-exports this much time before the previous
	// export's end, to pick up conversations Vault indexed late.
	geminiExportOverlap = 48 * time.Hour
	// geminiMinExportWindow is the shortest window a too-large export is split
	// to. A window this short that is still too large fails the sync.
	geminiMinExportWindow = time.Hour
	// geminiPendingRecheck is how soon the scheduler looks at an unfinished
	// Vault export again. It is just under the 5-minute coordinator interval,
	// so the next coordinator run picks the sync up, not the one after it.
	geminiPendingRecheck = 4 * time.Minute
	// Bound total memory even when Google splits a large export into many files.
	geminiMaxExportBytes   = 128 << 20
	geminiMessageBatchSize = 100
	geminiCursorVersion    = 2
	geminiExportNameTime   = "20060102T150405Z"
)

type geminiVaultClient interface {
	FindExport(context.Context, string) (*vault.Export, error)
	CreateExport(context.Context, string, time.Time, time.Time) (*vault.Export, error)
	GetExport(context.Context, string) (*vault.Export, error)
	Download(context.Context, vault.File) ([]byte, error)
	DeleteExport(context.Context, string) error
}

// GeminiSyncResult separates source progress from scheduling time. Pending
// exports MUST NOT advance Watermark or schedule the next poll from that old
// watermark: an asynchronous export can span several worker invocations.
type GeminiSyncResult struct {
	Cursor    string
	Watermark time.Time
	// Pending means a Vault export is still running or still has to be
	// started, so the scheduler checks again in geminiPendingRecheck.
	Pending bool
}

// geminiExportCursor is the sync progress saved in last_cursor_id.
//
// Version 1 cursors imported one completed UTC day per export, so Through was
// always a UTC midnight. Version 2 exports from Through to the poll time in
// one export. Both decode; the sync writes version 2.
type geminiExportCursor struct {
	Version int `json:"version"`
	// Through is the exclusive end of the last imported window.
	Through time.Time `json:"through"`
	// Sequence counts the exports started. It keeps export names unique.
	Sequence uint64 `json:"sequence"`
	// Overlap: the next window starts geminiExportOverlap before Through. It
	// is set after a window that ended at its poll time. The first window and
	// catch-up windows continue at Through.
	Overlap bool `json:"overlap,omitempty"`
	// ExportID, Start and End are the pending window. End is zero when no
	// window is pending; ExportID is empty until its export is started.
	ExportID string    `json:"export_id,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	// Live: the pending window ends at the poll time that started it, so the
	// sync is caught up once it is imported.
	Live bool `json:"live,omitempty"`
}

// GeminiImportService imports managed Gemini app text transcripts from Vault.
// One invocation starts or resumes one export that runs from the saved
// progress to the poll time; no workflow or timer is created per conversation.
// Export identifiers live in the sync cursor so activity retries and worker
// restarts resume the same remote job.
type GeminiImportService struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	writer      *chat.ChatMessageWriter
	newClient   func(Config) (geminiVaultClient, error)
	heartbeat   func(context.Context, string, int)
	importFiles func(context.Context, Config, []geminiExportFile) error
}

func NewGeminiImportService(logger *slog.Logger, db *pgxpool.Pool, policy *guardian.Policy, writer *chat.ChatMessageWriter, heartbeat func(context.Context, string, int)) *GeminiImportService {
	s := &GeminiImportService{
		logger: logger,
		db:     db,
		writer: writer,
		newClient: func(cfg Config) (geminiVaultClient, error) {
			if cfg.ExternalOrganizationID == nil {
				return nil, errors.New("gemini Vault matter ID is required")
			}
			if cfg.Settings.OrgUnitID == "" {
				return nil, errors.New("gemini sync requires a selected organizational unit")
			}
			return vault.NewClient(vault.Config{
				ServiceAccountJSON: cfg.APIKey,
				DelegatedUser:      cfg.Settings.DelegatedUser,
				MatterID:           *cfg.ExternalOrganizationID,
				OrgUnitID:          cfg.Settings.OrgUnitID,
			}, policy)
		},
		heartbeat:   heartbeat,
		importFiles: nil,
	}
	s.importFiles = s.writeExport
	return s
}

func (s *GeminiImportService) SyncGemini(ctx context.Context, cfg Config, endTime time.Time) (GeminiSyncResult, error) {
	if cfg.Provider != ProviderGeminiCompliance {
		return GeminiSyncResult{}, errors.New("gemini import requires a Gemini compliance config")
	}
	now := endTime.UTC()
	cursor, err := decodeGeminiCursor(cfg.LastCursor, geminiInitialThrough(cfg, now))
	if err != nil {
		return GeminiSyncResult{}, err
	}
	result := GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: cursor.Through, Pending: false}
	if cursor.Through.After(now) {
		return result, errors.New("gemini sync cursor is ahead of the current time")
	}
	client, err := s.newClient(cfg)
	if err != nil {
		return result, fmt.Errorf("configure Gemini Vault client: %w", err)
	}
	s.beat(ctx, "export", 0)
	var export *vault.Export
	switch {
	case cursor.End.IsZero():
		next, ok := cursor.nextWindow(now)
		if !ok {
			return encodeGeminiResult(cursor, false)
		}
		cursor = next
		export, err = s.startExport(ctx, client, cfg.ID, &cursor)
	case cursor.ExportID == "":
		export, err = s.startExport(ctx, client, cfg.ID, &cursor)
	default:
		export, err = client.GetExport(ctx, cursor.ExportID)
		var httpErr *vault.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			// Expired/deleted exports are recreated for the SAME window, not skipped.
			cursor.ExportID = ""
			export, err = s.startExport(ctx, client, cfg.ID, &cursor)
		} else if err != nil {
			err = fmt.Errorf("read Gemini Vault export: %w", err)
		}
	}
	if err != nil {
		return result, err
	}
	switch export.Status {
	case "IN_PROGRESS":
		return encodeGeminiResult(cursor, true)
	case "COMPLETED":
		// Validate every artifact and error/count report before advancing progress.
	case "FAILED":
		// Remove only our failed job, allowing the next retry to recreate this
		// window. Surface the failure now rather than declaring a successful sync.
		if err := client.DeleteExport(ctx, export.ID); err != nil {
			return result, fmt.Errorf("remove failed Gemini Vault export: %w", err)
		}
		return result, errors.New("gemini Vault export failed; the same window will be retried")
	default:
		return result, errors.New("gemini Vault export returned an unsupported status")
	}
	if export.TotalArtifactCount < 0 || export.ExportedArtifactCount < 0 {
		return result, errors.New("invalid gemini export artifact counts")
	}
	if export.TotalArtifactCount > export.ExportedArtifactCount {
		if err := client.DeleteExport(ctx, export.ID); err != nil {
			return result, fmt.Errorf("remove partial Gemini Vault export: %w", err)
		}
		return result, fmt.Errorf("incomplete Gemini Vault export: %w", errGeminiPartialExport)
	}
	if len(export.Files) == 0 {
		return result, errors.New("completed gemini Vault export has no downloadable artifacts")
	}
	if geminiExportBytes(export) > geminiMaxExportBytes {
		return s.splitExport(ctx, client, cfg, cursor, result)
	}
	files := make([]geminiExportFile, 0, len(export.Files))
	total := 0
	for i, file := range export.Files {
		s.beat(ctx, "download", i)
		body, err := client.Download(ctx, file)
		if errors.Is(err, vault.ErrTooLarge) {
			// Vault reported no size, or a wrong one, for a file over the bound.
			return s.splitExport(ctx, client, cfg, cursor, result)
		}
		if err != nil {
			return result, fmt.Errorf("download Gemini Vault export artifact: %w", err)
		}
		total += len(body)
		if total > geminiMaxExportBytes {
			return s.splitExport(ctx, client, cfg, cursor, result)
		}
		files = append(files, geminiExportFile{Name: path.Base(file.ObjectName), Content: body})
	}
	if err := s.importFiles(ctx, cfg, files); err != nil {
		if errors.Is(err, errGeminiExportTooLarge) {
			return s.splitExport(ctx, client, cfg, cursor, result)
		}
		if errors.Is(err, errGeminiPartialExport) {
			// An immutable partial source export cannot improve on retry.
			// Recreate this exact window next time; retain artifacts on local
			// parse/write failures so a code fix can safely replay them.
			if deleteErr := client.DeleteExport(ctx, export.ID); deleteErr != nil {
				return result, errors.Join(fmt.Errorf("import Gemini Vault transcripts: %w", err), fmt.Errorf("remove partial Gemini Vault export: %w", deleteErr))
			}
		}
		return result, fmt.Errorf("import Gemini Vault transcripts: %w", err)
	}
	cursor.Through = cursor.End
	cursor.Overlap = cursor.Live
	cursor.ExportID, cursor.Start, cursor.End, cursor.Live = "", time.Time{}, time.Time{}, false
	if cursor.Overlap {
		return encodeGeminiResult(cursor, false)
	}
	// The window ended before its poll time: it was split, or it is a
	// version 1 daily window. Export the rest now, not at the next hourly poll.
	next, ok := cursor.nextWindow(now)
	if !ok {
		return encodeGeminiResult(cursor, false)
	}
	return s.startNextExport(ctx, client, cfg, next)
}

// splitExport replaces a completed export that is too large to import with a
// shorter window. Conversations it already wrote stay; imports are idempotent.
func (s *GeminiImportService) splitExport(ctx context.Context, client geminiVaultClient, cfg Config, cursor geminiExportCursor, result GeminiSyncResult) (GeminiSyncResult, error) {
	next, ok := cursor.shrink()
	if !ok {
		return result, errors.New("gemini Vault export exceeds the import size limit even for one hour; narrow the export scope")
	}
	// The next window has a new name, so this job is only clean-up.
	if err := client.DeleteExport(ctx, cursor.ExportID); err != nil {
		s.warn(ctx, "remove oversized Gemini Vault export", err)
	}
	return s.startNextExport(ctx, client, cfg, next)
}

// startNextExport starts the export for a window chosen after progress that
// must not be lost. If Vault refuses, the window is saved without an export
// and the next run starts it again, and reports the error if it repeats.
func (s *GeminiImportService) startNextExport(ctx context.Context, client geminiVaultClient, cfg Config, cursor geminiExportCursor) (GeminiSyncResult, error) {
	if _, err := s.startExport(ctx, client, cfg.ID, &cursor); err != nil {
		s.warn(ctx, "start next Gemini Vault export", err)
		cursor.ExportID = ""
	}
	return encodeGeminiResult(cursor, true)
}

// startExport finds or creates the export for the cursor's window and records
// its ID. The name depends only on saved progress, so a retry after a lost
// create response finds the job and takes the window Vault exported.
//
// A found job is used only when Vault reports its window: without the window,
// the import cannot say where the export ends, and could skip the time between
// the job's end and the retry. Such a job is replaced by one under the next
// name, whose window is the one this call asks for.
func (s *GeminiImportService) startExport(ctx context.Context, client geminiVaultClient, configID uuid.UUID, cursor *geminiExportCursor) (*vault.Export, error) {
	for replaced := false; ; replaced = true {
		name := geminiExportName(configID, *cursor)
		export, err := client.FindExport(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("find Gemini Vault export: %w", err)
		}
		if export == nil {
			export, err = client.CreateExport(ctx, name, cursor.Start, cursor.End)
			if err != nil {
				return nil, fmt.Errorf("create Gemini Vault export: %w", err)
			}
			return recordGeminiExport(cursor, export)
		}
		if export.Start.Equal(cursor.Start) && export.End.After(cursor.Through) {
			cursor.End = export.End.UTC()
			return recordGeminiExport(cursor, export)
		}
		if replaced {
			return nil, errors.New("gemini Vault export does not report its time window")
		}
		if export.ID != "" {
			if err := client.DeleteExport(ctx, export.ID); err != nil {
				s.warn(ctx, "remove Gemini Vault export without a time window", err)
			}
		}
		cursor.Sequence++
	}
}

func recordGeminiExport(cursor *geminiExportCursor, export *vault.Export) (*vault.Export, error) {
	if export == nil || export.ID == "" {
		return nil, errors.New("gemini Vault returned an export without an ID")
	}
	cursor.ExportID = export.ID
	return export, nil
}

// nextWindow chooses the window after Through. Every new window ends at the
// poll time, in whole seconds, so the window Vault reports back compares equal.
// It returns false when there is no time left to export.
func (c geminiExportCursor) nextWindow(now time.Time) (geminiExportCursor, bool) {
	now = now.UTC().Truncate(time.Second)
	start := c.Through
	if c.Overlap {
		start = start.Add(-geminiExportOverlap)
	}
	if !now.After(c.Through) {
		return c, false
	}
	c.Version = geminiCursorVersion
	c.Sequence++
	c.ExportID, c.Start, c.End, c.Live = "", start, now, true
	return c, true
}

// shrink narrows a window whose export is too large. It first drops the
// overlap re-check, then halves the window, never below geminiMinExportWindow.
// The rest of the range follows in catch-up windows. It returns false when the
// window cannot get shorter.
func (c geminiExportCursor) shrink() (geminiExportCursor, bool) {
	if c.Start.Before(c.Through) {
		c.Start = c.Through
	} else {
		span := c.End.Sub(c.Start)
		if span <= geminiMinExportWindow {
			return c, false
		}
		c.End = c.Start.Add(max((span / 2).Truncate(time.Second), geminiMinExportWindow))
		c.Live = false
	}
	c.Version = geminiCursorVersion
	c.Sequence++
	c.ExportID = ""
	return c, true
}

// geminiInitialThrough is where an empty cursor starts: 30 days before the
// save that connected Gemini or last changed its key or organizational unit.
// Saves write updated_at and clear the cursor; retries reuse the same window.
func geminiInitialThrough(cfg Config, now time.Time) time.Time {
	anchor := cfg.UpdatedAt
	if anchor.IsZero() {
		anchor = cfg.CreatedAt
	}
	if anchor.IsZero() {
		anchor = now
	}
	return anchor.UTC().Truncate(time.Second).Add(-geminiInitialLookback)
}

// geminiExportBytes is the export size Vault reports, or the sum of its file
// sizes when Vault does not report one.
func geminiExportBytes(export *vault.Export) int64 {
	if export.SizeBytes > 0 {
		return export.SizeBytes
	}
	var total int64
	for _, file := range export.Files {
		total += file.Size
	}
	return total
}

func (s *GeminiImportService) beat(ctx context.Context, phase string, page int) {
	if s.heartbeat != nil {
		s.heartbeat(ctx, phase, page)
	}
}

func (s *GeminiImportService) warn(ctx context.Context, msg string, err error) {
	if s.logger != nil {
		s.logger.WarnContext(ctx, msg, attr.SlogError(err))
	}
}

func decodeGeminiCursor(raw string, initialThrough time.Time) (geminiExportCursor, error) {
	if raw == "" {
		return geminiExportCursor{Version: geminiCursorVersion, Through: initialThrough.UTC(), Sequence: 0, Overlap: false, ExportID: "", Start: time.Time{}, End: time.Time{}, Live: false}, nil
	}
	var cursor geminiExportCursor
	if len(raw) > 4096 || json.Unmarshal([]byte(raw), &cursor) != nil || cursor.Through.IsZero() {
		return cursor, errors.New("invalid gemini export sync cursor")
	}
	switch cursor.Version {
	case 1:
		if !cursor.Through.Equal(cursor.Through.UTC().Truncate(24 * time.Hour)) {
			return cursor, errors.New("invalid gemini cursor date boundary")
		}
		if cursor.ExportID != "" && (cursor.Start.IsZero() || cursor.End.IsZero() || !cursor.Start.Before(cursor.End) || cursor.End.Before(cursor.Through) || cursor.End.Sub(cursor.Through) > 24*time.Hour || cursor.Through.Sub(cursor.Start) > geminiExportOverlap) {
			return cursor, errors.New("invalid pending Gemini export window")
		}
		if cursor.ExportID == "" {
			cursor.Start, cursor.End = time.Time{}, time.Time{}
		}
		// A daily window ends before its poll time, so it is never live.
		cursor.Overlap, cursor.Live = false, false
	case geminiCursorVersion:
		if cursor.End.IsZero() {
			if cursor.ExportID != "" || !cursor.Start.IsZero() {
				return cursor, errors.New("invalid pending Gemini export window")
			}
			break
		}
		if cursor.Start.IsZero() || cursor.Start.After(cursor.Through) || cursor.Through.Sub(cursor.Start) > geminiExportOverlap || !cursor.End.After(cursor.Through) {
			return cursor, errors.New("invalid pending Gemini export window")
		}
	default:
		return cursor, errors.New("invalid gemini export sync cursor")
	}
	return cursor, nil
}

func encodeGeminiResult(cursor geminiExportCursor, pending bool) (GeminiSyncResult, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return GeminiSyncResult{}, fmt.Errorf("encode Gemini export cursor: %w", err)
	}
	return GeminiSyncResult{Cursor: string(raw), Watermark: cursor.Through, Pending: pending}, nil
}

// geminiExportName depends on the window start and the export count, never on
// the poll time, so a retried create finds the job it already started.
func geminiExportName(configID uuid.UUID, cursor geminiExportCursor) string {
	return fmt.Sprintf("speakeasy-gemini-%s-%s-%d", configID, cursor.Start.UTC().Format(geminiExportNameTime), cursor.Sequence)
}

func geminiExternalChatID(matterID, owner, conversation string) string {
	// Scope IDs to the Workspace matter and owner; the chat table's external ID
	// uniqueness is project-wide, shared with other compliance providers.
	sum := sha256.Sum256([]byte(matterID + "\x00" + conv.NormalizeEmail(owner) + "\x00" + conversation))
	return "gemini:" + hex.EncodeToString(sum[:])
}

func (s *GeminiImportService) writeExport(ctx context.Context, cfg Config, files []geminiExportFile) error {
	users := newConnectedUserResolver(s.db, cfg.OrganizationID)
	chatIDs := map[string]uuid.UUID{}
	latestTurns := map[string]geminiTurn{}
	rows := make([]chat.ExternalMessageWrite, 0, geminiMessageBatchSize)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		s.beat(ctx, "write", len(chatIDs))
		if _, err := s.writer.WriteExternal(ctx, cfg.ProjectID, rows); err != nil {
			return fmt.Errorf("write Gemini transcript messages: %w", err)
		}
		rows = rows[:0]
		return nil
	}
	err := parseGeminiExport(ctx, files, func(turn geminiTurn) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("visit gemini transcript: %w", err)
		}
		userID, err := users.resolve(ctx, turn.OwnerEmail)
		if err != nil {
			return err
		}
		externalID := geminiExternalChatID(*cfg.ExternalOrganizationID, turn.OwnerEmail, turn.ConversationID)
		chatID, known := chatIDs[externalID]
		if !known {
			upserted, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
				ID: uuid.New(), ProjectID: cfg.ProjectID, OrganizationID: cfg.OrganizationID,
				UserID: conv.ToPGTextEmpty(userID), ExternalUserID: conv.ToPGText(conv.NormalizeEmail(turn.OwnerEmail)),
				ExternalChatID: conv.ToPGText(externalID), Title: conv.ToPGTextEmpty(conv.StripNUL(turn.Title)),
				CreatedAt: conv.ToPGTimestamptz(turn.Timestamp), UpdatedAt: conv.ToPGTimestamptz(turn.Timestamp), PreferStoredTitle: false,
			})
			if err != nil {
				return fmt.Errorf("upsert Gemini conversation: %w", err)
			}
			chatID = upserted.ID
			if _, err := chatrepo.New(s.db).LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{AiIntegrationConfigID: cfg.ID, ChatID: chatID, ProjectID: cfg.ProjectID}); err != nil {
				return fmt.Errorf("link Gemini conversation: %w", err)
			}
			chatIDs[externalID] = chatID
		}
		// The parser visits each conversation oldest-first. Preserve the newest
		// nonempty title and last activity without issuing an upsert per turn.
		latest := turn
		if latest.Title == "" {
			latest.Title = latestTurns[externalID].Title
		}
		latestTurns[externalID] = latest
		rows = append(rows, geminiMessageRows(cfg, chatID, userID, turn)...)
		if len(rows) >= geminiMessageBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	for externalID, latest := range latestTurns {
		userID, err := users.resolve(ctx, latest.OwnerEmail)
		if err != nil {
			return err
		}
		s.beat(ctx, "metadata", len(chatIDs))
		if _, err := chatrepo.New(s.db).UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
			ID: chatIDs[externalID], ProjectID: cfg.ProjectID, OrganizationID: cfg.OrganizationID,
			UserID: conv.ToPGTextEmpty(userID), ExternalUserID: conv.ToPGText(conv.NormalizeEmail(latest.OwnerEmail)),
			ExternalChatID: conv.ToPGText(externalID), Title: conv.ToPGTextEmpty(conv.StripNUL(latest.Title)),
			CreatedAt: conv.ToPGTimestamptz(latest.Timestamp), UpdatedAt: conv.ToPGTimestamptz(latest.Timestamp), PreferStoredTitle: false,
		}); err != nil {
			return fmt.Errorf("update Gemini conversation metadata: %w", err)
		}
	}
	return nil
}

func geminiMessageRows(cfg Config, chatID uuid.UUID, userID string, turn geminiTurn) []chat.ExternalMessageWrite {
	rows := make([]chat.ExternalMessageWrite, 0, 2)
	for _, message := range []struct{ role, content string }{{"user", turn.Prompt}, {"assistant", turn.Response}} {
		if strings.TrimSpace(message.content) == "" {
			continue
		}
		rows = append(rows, chat.ExternalMessageWrite{
			Params: chatrepo.CreateExternalChatMessageParams{
				ID: uuid.Nil, ChatID: chatID, Role: message.role, ProjectID: cfg.ProjectID, Content: conv.StripNUL(message.content), ContentRaw: nil,
				ContentAssetUrl: pgtype.Text{String: "", Valid: false}, StorageError: pgtype.Text{String: "", Valid: false},
				Model: conv.ToPGTextEmpty(turn.Model), MessageID: pgtype.Text{String: "", Valid: false}, ToolCallID: pgtype.Text{String: "", Valid: false},
				UserID: conv.ToPGTextEmpty(userID), ExternalUserID: conv.ToPGText(conv.NormalizeEmail(turn.OwnerEmail)),
				ExternalMessageID: conv.ToPGText("gemini:" + turn.ID + ":" + message.role), FinishReason: pgtype.Text{String: "", Valid: false},
				ToolCalls: nil, PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0, Origin: pgtype.Text{String: "", Valid: false},
				UserAgent: pgtype.Text{String: "", Valid: false}, IpAddress: pgtype.Text{String: "", Valid: false}, Source: conv.ToPGText("gemini"),
				ContentHash: nil, Generation: 0, CreatedAt: conv.ToPGTimestamptz(turn.Timestamp),
			},
			BillingUserID: userID, WorkloadSource: metering.WorkloadSourceImport, UserEmail: turn.OwnerEmail, Provider: "google", InferenceProvider: "google", HookHostname: "", AccountType: complianceAccountTypeTeam, BillingMode: cfg.BillingMode,
		})
	}
	return rows
}
