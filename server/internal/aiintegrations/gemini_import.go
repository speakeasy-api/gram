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

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/metering"
	vault "github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
)

const (
	geminiInitialLookback = 30 * 24 * time.Hour
	geminiExportOverlap   = 48 * time.Hour
	// Bound total memory even when Google splits a large export into many files.
	geminiMaxExportBytes   = 128 << 20
	geminiMessageBatchSize = 100
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
}

type geminiExportCursor struct {
	Version  int       `json:"version"`
	Through  time.Time `json:"through"`
	Sequence uint64    `json:"sequence"`
	ExportID string    `json:"export_id,omitempty"`
	Start    time.Time `json:"start,omitempty"`
	End      time.Time `json:"end,omitempty"`
}

// GeminiImportService imports managed Gemini app text transcripts from Vault.
// One invocation starts or resumes one daily export; no workflow or timer is
// created per conversation. Export identifiers live in the sync cursor so
// activity retries and worker restarts resume the same remote job.
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
			return vault.NewClient(cfg.APIKey, *cfg.ExternalOrganizationID, policy)
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
	// Anchor the first window to durable connection time, not retry time.
	initialAt := cfg.CreatedAt
	if initialAt.IsZero() {
		initialAt = endTime
	}
	cursor, err := decodeGeminiCursor(cfg.LastCursor, initialAt)
	if err != nil {
		return GeminiSyncResult{}, err
	}
	result := GeminiSyncResult{Cursor: cfg.LastCursor, Watermark: cursor.Through}
	client, err := s.newClient(cfg)
	if err != nil {
		return result, fmt.Errorf("configure Gemini Vault client: %w", err)
	}
	s.beat(ctx, "export", 0)
	var export *vault.Export
	if cursor.ExportID != "" {
		export, err = client.GetExport(ctx, cursor.ExportID)
		var httpErr *vault.HTTPError
		if err != nil && (!errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound) {
			return result, fmt.Errorf("read Gemini Vault export: %w", err)
		}
		// Expired/deleted exports are recreated for the SAME window, not skipped.
		if err != nil {
			export = nil
		}
	}
	if export == nil {
		if cursor.ExportID == "" {
			upper := endTime.UTC().Truncate(24 * time.Hour)
			if cursor.Through.After(upper) {
				return result, errors.New("gemini sync cursor is ahead of the current UTC date")
			}
			cursor.Start = cursor.Through.Add(-geminiExportOverlap)
			cursor.End = cursor.Through.Add(24 * time.Hour)
			if cursor.End.After(upper) {
				cursor.End = upper
			}
		}
		// Include the sequence so caught-up rechecks produce a fresh export, but
		// a retried create with a lost response finds the original job by name.
		name := geminiExportName(cfg.ID, cursor)
		export, err = client.FindExport(ctx, name)
		if err != nil {
			return result, fmt.Errorf("find Gemini Vault export: %w", err)
		}
		if export == nil {
			export, err = client.CreateExport(ctx, name, cursor.Start, cursor.End)
			if err != nil {
				return result, fmt.Errorf("create Gemini Vault export: %w", err)
			}
		}
	}
	if export == nil || export.ID == "" {
		return result, errors.New("gemini Vault returned an export without an ID")
	}
	cursor.ExportID = export.ID
	switch export.Status {
	case "IN_PROGRESS":
		return encodeGeminiResult(cursor)
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
	files := make([]geminiExportFile, 0, len(export.Files))
	total := 0
	for i, file := range export.Files {
		s.beat(ctx, "download", i)
		body, err := client.Download(ctx, file)
		if err != nil {
			return result, fmt.Errorf("download Gemini Vault export artifact: %w", err)
		}
		total += len(body)
		if total > geminiMaxExportBytes {
			return result, errors.New("gemini Vault export exceeds the import size limit; narrow the export scope")
		}
		files = append(files, geminiExportFile{Name: path.Base(file.ObjectName), Content: body})
	}
	if err := s.importFiles(ctx, cfg, files); err != nil {
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
	cursor.Sequence++
	cursor.ExportID = ""
	cursor.Start = time.Time{}
	cursor.End = time.Time{}
	return encodeGeminiResult(cursor)
}

func (s *GeminiImportService) beat(ctx context.Context, phase string, page int) {
	if s.heartbeat != nil {
		s.heartbeat(ctx, phase, page)
	}
}

func decodeGeminiCursor(raw string, now time.Time) (geminiExportCursor, error) {
	if raw == "" {
		return geminiExportCursor{Version: 1, Through: now.UTC().Truncate(24 * time.Hour).Add(-geminiInitialLookback), Sequence: 0, ExportID: "", Start: time.Time{}, End: time.Time{}}, nil
	}
	var cursor geminiExportCursor
	if len(raw) > 4096 || json.Unmarshal([]byte(raw), &cursor) != nil || cursor.Version != 1 || cursor.Through.IsZero() {
		return cursor, errors.New("invalid gemini export sync cursor")
	}
	if !cursor.Through.Equal(cursor.Through.UTC().Truncate(24 * time.Hour)) {
		return cursor, errors.New("invalid gemini cursor date boundary")
	}
	if cursor.ExportID != "" && (cursor.Start.IsZero() || cursor.End.IsZero() || !cursor.Start.Before(cursor.End) || cursor.End.Before(cursor.Through) || cursor.End.Sub(cursor.Through) > 24*time.Hour || cursor.Through.Sub(cursor.Start) > geminiExportOverlap) {
		return cursor, errors.New("invalid pending Gemini export window")
	}
	return cursor, nil
}

func encodeGeminiResult(cursor geminiExportCursor) (GeminiSyncResult, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return GeminiSyncResult{}, fmt.Errorf("encode Gemini export cursor: %w", err)
	}
	return GeminiSyncResult{Cursor: string(raw), Watermark: cursor.Through}, nil
}

func geminiExportName(configID uuid.UUID, cursor geminiExportCursor) string {
	return fmt.Sprintf("speakeasy-gemini-%s-%s-%d", configID, cursor.End.Format("20060102"), cursor.Sequence)
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
