package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	chat_repo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// MCPSessionStorage handles persisting MCP sessions and messages to the chat tables.
type MCPSessionStorage struct {
	chatRepo *chat_repo.Queries
}

// NewMCPSessionStorage creates a new session storage instance.
func NewMCPSessionStorage(db *pgxpool.Pool) *MCPSessionStorage {
	return &MCPSessionStorage{
		chatRepo: chat_repo.New(db),
	}
}

// UpsertSessionParams contains parameters for upserting an MCP session.
type UpsertSessionParams struct {
	SessionID             uuid.UUID
	ProjectID             uuid.UUID
	OrganizationID        string
	UserID                string
	ExternalUserID        string
	Title                 string // Typically derived from first user message or intent
	ConnectionFingerprint string
}

// UpsertSession creates or updates an MCP session record in the chats table.
func (s *MCPSessionStorage) UpsertSession(ctx context.Context, params UpsertSessionParams) error {
	_, err := s.chatRepo.UpsertMCPSession(ctx, chat_repo.UpsertMCPSessionParams{
		ID:                    params.SessionID,
		ProjectID:             params.ProjectID,
		OrganizationID:        params.OrganizationID,
		UserID:                conv.ToPGText(params.UserID),
		ExternalUserID:        conv.ToPGText(params.ExternalUserID),
		Title:                 conv.ToPGText(params.Title),
		ConnectionFingerprint: conv.ToPGText(params.ConnectionFingerprint),
	})
	if err != nil {
		return fmt.Errorf("upsert MCP session: %w", err)
	}
	return nil
}

// StoreMessagesParams contains parameters for storing conversation messages.
type StoreMessagesParams struct {
	SessionID      uuid.UUID
	ProjectID      uuid.UUID
	Messages       []MCPMessage
	UserID         string
	ExternalUserID string
	Origin         string
	UserAgent      string
	IPAddress      string
}

// StoreMessages stores conversation messages from x-gram-messages as chat_messages records.
func (s *MCPSessionStorage) StoreMessages(ctx context.Context, params StoreMessagesParams) error {
	if len(params.Messages) == 0 {
		return nil
	}

	var chatMessages []chat_repo.CreateChatMessageParams
	for _, msg := range params.Messages {
		// Skip invalid messages
		if msg.Role == "" || msg.Content == "" {
			continue
		}
		// Only accept user and assistant roles
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}

		chatMessages = append(chatMessages, chat_repo.CreateChatMessageParams{
			ChatID:           params.SessionID,
			ProjectID:        params.ProjectID,
			Role:             msg.Role,
			Content:          msg.Content,
			ContentRaw:       nil,
			ContentAssetUrl:  conv.ToPGText(""),
			StorageError:     conv.ToPGText(""),
			Model:            conv.ToPGText(""),
			MessageID:        conv.ToPGText(""),
			ToolCallID:       conv.ToPGText(""),
			UserID:           conv.ToPGText(params.UserID),
			ExternalUserID:   conv.ToPGText(params.ExternalUserID),
			FinishReason:     conv.ToPGText(""),
			ToolCalls:        nil,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           conv.ToPGText(params.Origin),
			UserAgent:        conv.ToPGText(params.UserAgent),
			IpAddress:        conv.ToPGText(params.IPAddress),
			Source:           conv.ToPGText("MCP"),
		})
	}

	if len(chatMessages) == 0 {
		return nil
	}

	_, err := s.chatRepo.CreateChatMessage(ctx, chatMessages)
	if err != nil {
		return fmt.Errorf("create chat messages: %w", err)
	}

	return nil
}

// StoreToolCallParams contains parameters for storing a tool call as a message.
type StoreToolCallParams struct {
	SessionID      uuid.UUID
	ProjectID      uuid.UUID
	ToolName       string
	ToolURN        string
	ToolCallID     string
	Request        json.RawMessage
	Response       string
	UserID         string
	ExternalUserID string
	Origin         string
	UserAgent      string
	IPAddress      string
}

// StoreToolCall stores a tool call response as a chat_message record with role='tool'.
func (s *MCPSessionStorage) StoreToolCall(ctx context.Context, params StoreToolCallParams) error {
	// Format content as tool response
	content := params.Response
	if content == "" {
		content = "(empty response)"
	}

	_, err := s.chatRepo.CreateChatMessage(ctx, []chat_repo.CreateChatMessageParams{
		{
			ChatID:           params.SessionID,
			ProjectID:        params.ProjectID,
			Role:             "tool",
			Content:          content,
			ContentRaw:       nil,
			ContentAssetUrl:  conv.ToPGText(""),
			StorageError:     conv.ToPGText(""),
			Model:            conv.ToPGText(""),
			MessageID:        conv.ToPGText(""),
			ToolCallID:       conv.ToPGText(params.ToolCallID),
			UserID:           conv.ToPGText(params.UserID),
			ExternalUserID:   conv.ToPGText(params.ExternalUserID),
			FinishReason:     conv.ToPGText(""),
			ToolCalls:        nil,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           conv.ToPGText(params.Origin),
			UserAgent:        conv.ToPGText(params.UserAgent),
			IpAddress:        conv.ToPGText(params.IPAddress),
			Source:           conv.ToPGText("MCP"),
		},
	})
	if err != nil {
		return fmt.Errorf("create tool call message: %w", err)
	}

	return nil
}

// deriveSessionTitle extracts a title from the first user message in the session.
func deriveSessionTitle(messages []MCPMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Content != "" {
			// Truncate to reasonable length for title
			title := msg.Content
			if len(title) > 100 {
				title = title[:97] + "..."
			}
			return title
		}
	}
	return ""
}
