package aiintegrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	anthropicComplianceActivityCreated = "claude_chat_created"
	anthropicComplianceActivityUpdated = "claude_chat_updated"
	anthropicComplianceSource          = "anthropic_compliance"
	anthropicCompliancePageLimit       = 1000
	maxInlineExternalContentSize       = 128 * 1024
	maxComplianceAssetBytes            = 100 * 1024 * 1024
)

type ComplianceImportService struct {
	logger         *slog.Logger
	guardianPolicy *guardian.Policy
	chatRepo       *chatrepo.Queries
	users          *usersrepo.Queries
	writer         *chat.ChatMessageWriter
	assetStorage   assets.BlobStore
	heartbeat      func(ctx context.Context, scope string, page int)
}

func NewComplianceImportService(logger *slog.Logger, db *pgxpool.Pool, guardianPolicy *guardian.Policy, writer *chat.ChatMessageWriter, assetStorage assets.BlobStore, heartbeat func(ctx context.Context, scope string, page int)) *ComplianceImportService {
	if guardianPolicy == nil {
		panic("anthropic compliance import service requires guardian policy")
	}
	if writer == nil {
		panic("anthropic compliance import service requires chat message writer")
	}
	if assetStorage == nil {
		panic("anthropic compliance import service requires asset storage")
	}
	if heartbeat == nil {
		panic("anthropic compliance import service requires heartbeat")
	}
	return &ComplianceImportService{
		logger:         logger.With(attr.SlogComponent("aiintegrations.anthropic_compliance")),
		guardianPolicy: guardianPolicy,
		chatRepo:       chatrepo.New(db),
		users:          usersrepo.New(db),
		writer:         writer,
		assetStorage:   assetStorage,
		heartbeat:      heartbeat,
	}
}

func (s *ComplianceImportService) SyncAnthropicCompliance(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderAnthropicCompliance {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for compliance import: %s", cfg.Provider)
	}
	if cfg.ExternalOrganizationID == "" {
		return oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
	}

	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey))
	activities, err := s.discoverChatActivities(ctx, client, cfg, endTime)
	if err != nil {
		return err
	}
	if len(activities) == 0 {
		return nil
	}

	userIDsByEmail, err := s.hydrateConnectedUsers(ctx, cfg.OrganizationID, activityEmails(activities))
	if err != nil {
		return err
	}

	for _, activity := range activities {
		s.heartbeat(ctx, "chat_import", 0)
		chatID, err := s.upsertActivityChat(ctx, cfg, activity, userIDsByEmail)
		if err != nil {
			return err
		}
		if err := s.importChatMessages(ctx, client, cfg, chatID, activity.ClaudeChatID, userIDsByEmail); err != nil {
			return err
		}
	}
	return nil
}

func (s *ComplianceImportService) discoverChatActivities(ctx context.Context, client *anthropicapi.Client, cfg Config, endTime time.Time) ([]anthropicapi.Activity, error) {
	seen := map[string]anthropicapi.Activity{}
	var afterID string
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "activity_discovery", pageNum)
		page, err := client.ListActivities(ctx, anthropicapi.ListActivitiesParams{
			ActivityTypes: []string{
				anthropicComplianceActivityCreated,
				anthropicComplianceActivityUpdated,
			},
			OrganizationIDs: []string{cfg.ExternalOrganizationID},
			CreatedAtGTE:    cfg.PollWatermarkAt,
			AfterID:         afterID,
			Limit:           5000,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list anthropic compliance activities")
		}
		for _, activity := range page.Data {
			if activity.Actor.Type != "user_actor" || activity.ClaudeChatID == "" {
				continue
			}
			createdAt, err := activity.CreatedAtTime()
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "parse anthropic compliance activity timestamp")
			}
			if !createdAt.IsZero() && createdAt.After(endTime) {
				continue
			}
			seen[activity.ClaudeChatID] = activity
		}
		if !page.HasMore || page.LastID == "" {
			break
		}
		afterID = page.LastID
	}

	activities := make([]anthropicapi.Activity, 0, len(seen))
	for _, activity := range seen {
		activities = append(activities, activity)
	}
	return activities, nil
}

func (s *ComplianceImportService) upsertActivityChat(ctx context.Context, cfg Config, activity anthropicapi.Activity, userIDsByEmail map[string]string) (uuid.UUID, error) {
	createdAt, err := activity.CreatedAtTime()
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "parse anthropic compliance activity timestamp")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	userID := userIDsByEmail[normalizeEmail(activity.Actor.EmailAddress)]
	chatID, err := s.chatRepo.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             uuid.New(),
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(activity.Actor.UserID),
		ExternalChatID: conv.ToPGText(activity.ClaudeChatID),
		Title:          conv.ToPGText("Claude Chat"),
		CreatedAt:      timestamptz(createdAt),
		UpdatedAt:      timestamptz(createdAt),
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "upsert anthropic compliance chat")
	}
	if err := s.chatRepo.LinkAIIntegrationConfigChat(ctx, chatrepo.LinkAIIntegrationConfigChatParams{
		AiIntegrationConfigID: cfg.ID,
		ChatID:                chatID,
	}); err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "link anthropic compliance chat")
	}
	return chatID, nil
}

func (s *ComplianceImportService) importChatMessages(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, claudeChatID string, userIDsByEmail map[string]string) error {
	var afterID string
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "message_import", pageNum)
		page, err := client.GetChatMessages(ctx, anthropicapi.GetChatMessagesParams{
			ClaudeChatID: claudeChatID,
			AfterID:      afterID,
			Limit:        anthropicCompliancePageLimit,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "get anthropic compliance chat messages")
		}
		if err := s.upsertMessagePageChat(ctx, cfg, chatID, page, userIDsByEmail); err != nil {
			return err
		}
		rows, err := s.buildExternalMessageRows(ctx, client, cfg, chatID, page, userIDsByEmail)
		if err != nil {
			return err
		}
		if _, err := s.writer.WriteExternal(ctx, cfg.ProjectID, rows); err != nil {
			return oops.E(oops.CodeUnexpected, err, "write anthropic compliance chat messages")
		}
		if !page.HasMore || page.LastID == "" {
			break
		}
		afterID = page.LastID
	}
	return nil
}

func (s *ComplianceImportService) upsertMessagePageChat(ctx context.Context, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, userIDsByEmail map[string]string) error {
	createdAt := parseTimeOrNow(page.CreatedAt)
	updatedAt := parseTimeOrDefault(page.UpdatedAt, createdAt)
	userID := userIDsByEmail[normalizeEmail(page.User.EmailAddress)]
	resolvedChatID, err := s.chatRepo.UpsertExternalChat(ctx, chatrepo.UpsertExternalChatParams{
		ID:             chatID,
		ProjectID:      cfg.ProjectID,
		OrganizationID: cfg.OrganizationID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(page.User.ID),
		ExternalChatID: conv.ToPGText(page.ID),
		Title:          conv.ToPGText(page.Name),
		CreatedAt:      timestamptz(createdAt),
		UpdatedAt:      timestamptz(updatedAt),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "upsert anthropic compliance chat metadata")
	}
	if resolvedChatID != chatID {
		s.logger.WarnContext(ctx, "anthropic compliance chat resolved to different id",
			attr.SlogChatID(resolvedChatID.String()),
			attr.SlogAIIntegrationConfigID(cfg.ID.String()),
		)
	}
	return nil
}

func (s *ComplianceImportService) buildExternalMessageRows(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage, userIDsByEmail map[string]string) ([]chatrepo.CreateExternalChatMessageParams, error) {
	rows := make([]chatrepo.CreateExternalChatMessageParams, 0, len(page.Messages))
	userID := userIDsByEmail[normalizeEmail(page.User.EmailAddress)]
	model := ""
	if page.Model != nil {
		model = *page.Model
	}
	for _, msg := range page.Messages {
		if msg.ID == "" || (msg.Role != "user" && msg.Role != "assistant") {
			continue
		}
		createdAt, err := msg.CreatedAtTime()
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse anthropic compliance message timestamp")
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		assetsURL, err := s.storeMessageAssets(ctx, client, cfg, page.ID, msg)
		if err != nil {
			return nil, err
		}
		content := renderComplianceContent(msg.Content)
		var contentRaw []byte
		if len(msg.Content) > 0 && len(msg.Content) <= maxInlineExternalContentSize {
			contentRaw = msg.Content
		}
		rows = append(rows, chatrepo.CreateExternalChatMessageParams{
			ChatID:                       chatID,
			Role:                         msg.Role,
			ProjectID:                    cfg.ProjectID,
			Content:                      content,
			ContentRaw:                   contentRaw,
			ContentAssetUrl:              pgtype.Text{},
			StorageError:                 pgtype.Text{},
			Model:                        conv.ToPGText(model),
			MessageID:                    pgtype.Text{},
			ToolCallID:                   pgtype.Text{},
			UserID:                       conv.ToPGText(userID),
			ExternalUserID:               conv.ToPGText(page.User.ID),
			ExternalMessageID:            conv.ToPGText(msg.ID),
			ExternalChatMessageAssetsUrl: conv.ToPGText(assetsURL),
			FinishReason:                 pgtype.Text{},
			ToolCalls:                    nil,
			PromptTokens:                 0,
			CompletionTokens:             0,
			TotalTokens:                  0,
			Origin:                       conv.ToPGText(page.Href),
			UserAgent:                    pgtype.Text{},
			IpAddress:                    pgtype.Text{},
			Source:                       conv.ToPGText(anthropicComplianceSource),
			ContentHash:                  nil,
			Generation:                   0,
			CreatedAt:                    timestamptz(createdAt),
		})
	}
	return rows, nil
}

func (s *ComplianceImportService) storeMessageAssets(ctx context.Context, client *anthropicapi.Client, cfg Config, claudeChatID string, msg anthropicapi.ChatMessage) (string, error) {
	totalAssets := len(msg.Files) + len(msg.GeneratedFiles) + len(msg.Artifacts)
	if totalAssets == 0 {
		return "", nil
	}
	manifest := complianceAssetsManifest{
		Provider:  anthropicComplianceSource,
		ChatID:    claudeChatID,
		MessageID: msg.ID,
		Assets:    make([]complianceAssetManifestEntry, 0, totalAssets),
	}
	for _, file := range msg.Files {
		entry := s.downloadAndStoreAsset(ctx, cfg, claudeChatID, msg.ID, "file", file.ID, file.Filename, file.MIMEType, func(ctx context.Context) (*anthropicapi.DownloadedContent, error) {
			return client.DownloadChatFile(ctx, file.ID)
		})
		manifest.Assets = append(manifest.Assets, entry)
	}
	for _, file := range msg.GeneratedFiles {
		entry := s.downloadAndStoreAsset(ctx, cfg, claudeChatID, msg.ID, "generated_file", file.ID, file.Filename, file.MIMEType, func(ctx context.Context) (*anthropicapi.DownloadedContent, error) {
			return client.DownloadGeneratedFile(ctx, file.ID)
		})
		manifest.Assets = append(manifest.Assets, entry)
	}
	for _, artifact := range msg.Artifacts {
		entry := s.downloadAndStoreAsset(ctx, cfg, claudeChatID, msg.ID, "artifact", artifact.VersionID, artifact.Title, artifact.ArtifactType, func(ctx context.Context) (*anthropicapi.DownloadedContent, error) {
			return client.DownloadArtifact(ctx, artifact.VersionID)
		})
		entry.ArtifactID = artifact.ID
		entry.VersionID = artifact.VersionID
		manifest.Assets = append(manifest.Assets, entry)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "marshal anthropic compliance asset manifest")
	}
	hash := sha256.Sum256(data)
	manifestPath := path.Join(cfg.ProjectID.String(), "ai-integrations", anthropicComplianceSource, claudeChatID, msg.ID, hex.EncodeToString(hash[:])+".manifest.json")
	writer, assetURL, err := s.assetStorage.Write(ctx, manifestPath, "application/json", int64(len(data)))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "create anthropic compliance asset manifest")
	}
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		_ = writer.Close()
		return "", oops.E(oops.CodeUnexpected, err, "write anthropic compliance asset manifest")
	}
	if err := writer.Close(); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "finalize anthropic compliance asset manifest")
	}
	return assetURL.String(), nil
}

func (s *ComplianceImportService) downloadAndStoreAsset(ctx context.Context, cfg Config, claudeChatID, messageID, kind, id, filename, mimeType string, download func(context.Context) (*anthropicapi.DownloadedContent, error)) complianceAssetManifestEntry {
	entry := complianceAssetManifestEntry{
		Kind:     kind,
		ID:       id,
		Filename: filename,
		MIMEType: mimeType,
	}
	if id == "" {
		entry.Error = "missing asset id"
		return entry
	}
	downloaded, err := download(ctx)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	defer func() {
		_ = downloaded.Body.Close()
	}()
	if downloaded.Filename != "" {
		entry.Filename = downloaded.Filename
	}
	if downloaded.ContentType != "" {
		entry.MIMEType = downloaded.ContentType
	}
	data, err := io.ReadAll(io.LimitReader(downloaded.Body, maxComplianceAssetBytes+1))
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	if len(data) > maxComplianceAssetBytes {
		entry.Error = fmt.Sprintf("asset exceeds %d byte import limit", maxComplianceAssetBytes)
		return entry
	}
	hash := sha256.Sum256(data)
	entry.SHA256 = hex.EncodeToString(hash[:])
	assetPath := path.Join(cfg.ProjectID.String(), "ai-integrations", anthropicComplianceSource, claudeChatID, messageID, kind, id, entry.SHA256)
	writer, assetURL, err := s.assetStorage.Write(ctx, assetPath, entry.MIMEType, int64(len(data)))
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		_ = writer.Close()
		entry.Error = err.Error()
		return entry
	}
	if err := writer.Close(); err != nil {
		entry.Error = err.Error()
		return entry
	}
	entry.AssetURL = assetURL.String()
	return entry
}

type complianceAssetsManifest struct {
	Provider  string                         `json:"provider"`
	ChatID    string                         `json:"chat_id"`
	MessageID string                         `json:"message_id"`
	Assets    []complianceAssetManifestEntry `json:"assets"`
}

type complianceAssetManifestEntry struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	ArtifactID string `json:"artifact_id,omitempty"`
	VersionID  string `json:"version_id,omitempty"`
	Filename   string `json:"filename,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
	AssetURL   string `json:"asset_url,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Error      string `json:"error,omitempty"`
}

type complianceContentBlock struct {
	Type            string                   `json:"type"`
	Text            string                   `json:"text"`
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Input           json.RawMessage          `json:"input"`
	IntegrationName string                   `json:"integration_name"`
	MCPServerURL    string                   `json:"mcp_server_url"`
	ToolUseID       string                   `json:"tool_use_id"`
	IsError         bool                     `json:"is_error"`
	Truncated       bool                     `json:"truncated"`
	Content         []complianceContentBlock `json:"content"`
}

func renderComplianceContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var blocks []complianceContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_use":
			detail := strings.TrimSpace(block.Name)
			if len(block.Input) > 0 {
				detail = strings.TrimSpace(detail + " " + string(block.Input))
			}
			parts = append(parts, strings.TrimSpace("[tool_use "+detail+"]"))
		case "tool_result":
			texts := make([]string, 0, len(block.Content))
			for _, item := range block.Content {
				if item.Type == "text" && item.Text != "" {
					texts = append(texts, item.Text)
				}
			}
			if len(texts) > 0 {
				parts = append(parts, strings.Join(texts, "\n"))
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (s *ComplianceImportService) hydrateConnectedUsers(ctx context.Context, orgID string, emails []string) (map[string]string, error) {
	out := map[string]string{}
	if len(emails) == 0 {
		return out, nil
	}
	users, err := s.users.GetConnectedUsersByEmails(ctx, usersrepo.GetConnectedUsersByEmailsParams{
		Emails:         emails,
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "hydrate anthropic compliance users")
	}
	for _, user := range users {
		out[normalizeEmail(user.Email)] = user.ID
	}
	return out, nil
}

func activityEmails(activities []anthropicapi.Activity) []string {
	seen := map[string]struct{}{}
	for _, activity := range activities {
		email := normalizeEmail(activity.Actor.EmailAddress)
		if email != "" {
			seen[email] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for email := range seen {
		out = append(out, email)
	}
	return out
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func parseTimeOrNow(value string) time.Time {
	return parseTimeOrDefault(value, time.Now().UTC())
}

func parseTimeOrDefault(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback.UTC()
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fallback.UTC()
	}
	return t.UTC()
}

func IsAnthropicComplianceUnauthorized(err error) bool {
	var httpErr *anthropicapi.HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403)
}
