package agentcapture

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// chatExportRow is one dumped external chat. The compliance import lands
// claude.ai transcripts in Postgres (chats / chat_messages), not
// telemetry_logs, so the dump exports them as their own NDJSON files under
// chats/.
type chatExportRow struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	OrganizationID string    `json:"organization_id"`
	UserID         *string   `json:"user_id"`
	ExternalUserID *string   `json:"external_user_id"`
	ExternalChatID *string   `json:"external_chat_id"`
	Title          *string   `json:"title"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// chatMessageExportRow is one dumped external chat message. content_raw is
// deliberately not exported: the rendered content column is enough for
// fixtures and the raw blocks would need their own deep scrub.
type chatMessageExportRow struct {
	ID                string    `json:"id"`
	ChatID            string    `json:"chat_id"`
	Role              string    `json:"role"`
	Content           string    `json:"content"`
	Model             *string   `json:"model"`
	UserID            *string   `json:"user_id"`
	ExternalUserID    *string   `json:"external_user_id"`
	ExternalMessageID *string   `json:"external_message_id"`
	Origin            *string   `json:"origin"`
	UserAgent         *string   `json:"user_agent"`
	IPAddress         *string   `json:"ip_address"`
	Source            *string   `json:"source"`
	CreatedAt         time.Time `json:"created_at"`
}

// dumpExternalChats exports the project's provider-imported chats and
// messages for the capture window as chats/chats.ndjson and
// chats/messages.ndjson. Files are only created when the window has rows,
// but stale files from a previous capture are always cleared so the export
// never disagrees with the manifest.
func (s *Service) dumpExternalChats(ctx context.Context, project projectsrepo.Project, since, until time.Time, opts Options, anonymizer *Anonymizer) (map[string]int, error) {
	chatsDir := filepath.Join(opts.OutDir, "chats")
	stale, err := filepath.Glob(filepath.Join(chatsDir, "*.ndjson"))
	if err != nil {
		return nil, fmt.Errorf("list previous chat dump files: %w", err)
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove previous chat dump file: %w", err)
		}
	}

	q := chatrepo.New(s.db)
	chats, err := q.ListExternalChatsForExport(ctx, chatrepo.ListExternalChatsForExportParams{
		ProjectID: project.ID,
		Since:     conv.ToPGTimestamptz(since),
		Until:     conv.ToPGTimestamptz(until),
	})
	if err != nil {
		return nil, fmt.Errorf("list external chats: %w", err)
	}
	messages, err := q.ListExternalChatMessagesForExport(ctx, chatrepo.ListExternalChatMessagesForExportParams{
		ProjectID: project.ID,
		Since:     conv.ToPGTimestamptz(since),
		Until:     conv.ToPGTimestamptz(until),
	})
	if err != nil {
		return nil, fmt.Errorf("list external chat messages: %w", err)
	}
	if len(chats) == 0 && len(messages) == 0 {
		return map[string]int{}, nil
	}

	if err := os.MkdirAll(chatsDir, 0o750); err != nil {
		return nil, fmt.Errorf("create chat dump directory: %w", err)
	}

	chatRows := make([]any, 0, len(chats))
	for _, c := range chats {
		row := chatExportRow{
			ID:             c.ID.String(),
			ProjectID:      c.ProjectID.String(),
			OrganizationID: c.OrganizationID,
			UserID:         conv.FromPGText[string](c.UserID),
			ExternalUserID: conv.FromPGText[string](c.ExternalUserID),
			ExternalChatID: conv.FromPGText[string](c.ExternalChatID),
			Title:          conv.FromPGText[string](c.Title),
			CreatedAt:      c.CreatedAt.Time.UTC(),
			UpdatedAt:      c.UpdatedAt.Time.UTC(),
		}
		if anonymizer != nil {
			anonymizer.ScrubChat(&row)
		}
		chatRows = append(chatRows, row)
	}

	messageRows := make([]any, 0, len(messages))
	for _, m := range messages {
		row := chatMessageExportRow{
			ID:                m.ID.String(),
			ChatID:            m.ChatID.String(),
			Role:              m.Role,
			Content:           m.Content,
			Model:             conv.FromPGText[string](m.Model),
			UserID:            conv.FromPGText[string](m.UserID),
			ExternalUserID:    conv.FromPGText[string](m.ExternalUserID),
			ExternalMessageID: conv.FromPGText[string](m.ExternalMessageID),
			Origin:            conv.FromPGText[string](m.Origin),
			UserAgent:         conv.FromPGText[string](m.UserAgent),
			IPAddress:         conv.FromPGText[string](m.IpAddress),
			Source:            conv.FromPGText[string](m.Source),
			CreatedAt:         m.CreatedAt.Time.UTC(),
		}
		if anonymizer != nil {
			anonymizer.ScrubChatMessage(&row)
		}
		messageRows = append(messageRows, row)
	}

	files := map[string]int{}
	for name, rows := range map[string][]any{"chats": chatRows, "messages": messageRows} {
		if len(rows) == 0 {
			continue
		}
		if err := writeNDJSON(filepath.Join(chatsDir, name+".ndjson"), rows); err != nil {
			return nil, err
		}
		files[filepath.Join("chats", name+".ndjson")] = len(rows)
	}
	return files, nil
}

// writeNDJSON writes rows as newline-delimited JSON, one object per line.
func writeNDJSON(path string, rows []any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- dump files live inside the operator-chosen output directory
	if err != nil {
		return fmt.Errorf("create chat dump file %s: %w", path, err)
	}
	defer o11y.NoLogDefer(file.Close)

	buf := bufio.NewWriter(file)
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("serialize chat dump row: %w", err)
		}
		if _, err := buf.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write chat dump row: %w", err)
		}
	}
	if err := buf.Flush(); err != nil {
		return fmt.Errorf("flush chat dump file %s: %w", path, err)
	}
	return nil
}
