package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/claudetag"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// persistClaudeTagMetadata runs in the message transaction. It does not change
// ownership, billing, or authorization based on identities found in a prompt.
// Platform MCP assessment: list_chats already shares the inferred source query.
// Keep participant snapshots in the authorized management API; that MCP tool's
// masked-person contract deliberately excludes raw identities and transcripts,
// so neither a new tool nor unmasked participant fields belong in its result.
func persistClaudeTagMetadata(ctx context.Context, db repo.DBTX, param repo.CreateChatMessageParams) error {
	queries := repo.New(db)
	if err := queries.MarkKnownClaudeTagSubsession(ctx, repo.MarkKnownClaudeTagSubsessionParams{ProjectID: param.ProjectID, ChatID: param.ChatID}); err != nil {
		return fmt.Errorf("mark known Claude Tag subsession: %w", err)
	}
	if param.Role != "user" {
		return nil
	}
	metadata := claudetag.Parse(param.Content)
	if !metadata.Detected && param.Source.String != "claude-tag" {
		return nil
	}
	if err := queries.MarkClaudeTagMessages(ctx, repo.MarkClaudeTagMessagesParams{ProjectID: param.ProjectID, ChatID: param.ChatID}); err != nil {
		return fmt.Errorf("mark Claude Tag source: %w", err)
	}
	if metadata.ChannelID != "" {
		if err := queries.RecordChatSlackChannel(ctx, repo.RecordChatSlackChannelParams{ProjectID: param.ProjectID, ChatID: param.ChatID, TeamID: metadata.Team, ChannelID: metadata.ChannelID, ChannelName: metadata.ChannelName}); err != nil {
			return fmt.Errorf("record Slack channel: %w", err)
		}
	}
	for _, sender := range metadata.Senders {
		if err := queries.RecordSlackMessageParticipant(ctx, repo.RecordSlackMessageParticipantParams{ProjectID: param.ProjectID, MessageID: param.ID, ProviderUserID: sender, TeamID: metadata.Team}); err != nil {
			return fmt.Errorf("record Slack message participant: %w", err)
		}
	}
	if metadata.ChildSession != "" {
		if err := queries.LockSubsessionLinks(ctx, param.ProjectID.String()); err != nil {
			return fmt.Errorf("lock subsession links: %w", err)
		}
		// The envelope is delivered to the parent; from-session identifies its helper.
		if err := queries.InsertSubsessionLink(ctx, repo.InsertSubsessionLinkParams{ProjectID: param.ProjectID, ParentChatID: param.ChatID, ChildChatID: uuid.NullUUID{UUID: SessionIDToChatID(metadata.ChildSession), Valid: true}, ParentSessionID: param.ChatID.String(), ChildSessionID: conv.ToPGText(metadata.ChildSession)}); err != nil {
			return fmt.Errorf("record subsession link: %w", err)
		}
	}
	return nil
}

// chatParticipants loads only chats already admitted by the calling handler.
// Message attribution is bounded to the current transcript page.
func (s *Service) chatParticipants(ctx context.Context, projectID uuid.UUID, ids, messageIDs []uuid.UUID) (map[string][]*gen.ChatParticipant, map[string][]*gen.ChatParticipant, error) {
	rows, err := s.repo.ListChatParticipantRollups(ctx, repo.ListChatParticipantRollupsParams{ProjectID: projectID, ChatIds: ids})
	if err != nil {
		return nil, nil, fmt.Errorf("load chat participants: %w", err)
	}
	byChat := make(map[string][]*gen.ChatParticipant)
	byMessage := make(map[string][]*gen.ChatParticipant)
	seen := make(map[string]bool)
	for _, row := range rows {
		participant := &gen.ChatParticipant{Provider: row.Provider, ProviderUserID: row.ProviderUserID, ProviderTeamID: conv.FromPGText[string](row.ProviderTeamID), UserID: conv.FromPGText[string](row.UserID), DisplayName: conv.FromPGText[string](row.DisplayName)}
		chatID := row.ChatID.UUID.String()
		key := chatID + ":" + row.Provider + ":" + row.ProviderTeamID.String + ":" + row.ProviderUserID + ":" + row.UserID.String
		if !seen[key] {
			byChat[chatID] = append(byChat[chatID], participant)
			seen[key] = true
		}
	}
	if len(messageIDs) == 0 {
		return byChat, byMessage, nil
	}
	messages, err := s.repo.ListChatMessageParticipants(ctx, repo.ListChatMessageParticipantsParams{ProjectID: projectID, MessageIds: messageIDs})
	if err != nil {
		return nil, nil, fmt.Errorf("load message participants: %w", err)
	}
	for _, row := range messages {
		participant := &gen.ChatParticipant{Provider: row.Provider, ProviderUserID: row.ProviderUserID, ProviderTeamID: conv.FromPGText[string](row.ProviderTeamID), UserID: conv.FromPGText[string](row.UserID), DisplayName: conv.FromPGText[string](row.DisplayName)}
		messageID := row.MessageID.UUID.String()
		byMessage[messageID] = append(byMessage[messageID], participant)
	}
	return byChat, byMessage, nil
}
