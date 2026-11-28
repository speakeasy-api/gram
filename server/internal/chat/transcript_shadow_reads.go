package chat

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// messageBody points at the body fields of one loaded message row, so a chat
// read can swap them for the opened shadow whatever query loaded the row.
type messageBody struct {
	id              uuid.UUID
	chatID          uuid.UUID
	content         *string
	contentRaw      *[]byte
	toolCalls       *[]byte
	contentAssetURL pgtype.Text
}

func chatMessageBody(m *repo.ChatMessage) messageBody {
	return messageBody{id: m.ID, chatID: m.ChatID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls, contentAssetURL: m.ContentAssetUrl}
}

func riskWindowedMessageBody(m *repo.ListRiskWindowedMessagesRow) messageBody {
	return messageBody{id: m.ID, chatID: m.ChatID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls, contentAssetURL: m.ContentAssetUrl}
}

func searchWindowedMessageBody(m *repo.ListSearchWindowedMessagesRow) messageBody {
	return messageBody{id: m.ID, chatID: m.ChatID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls, contentAssetURL: m.ContentAssetUrl}
}

// openShadowRows replaces the body fields of a page of rows with their
// encrypted shadows when the caller's project reads through shadows, and leaves
// them untouched for every other project. A row with no shadow at all keeps its
// plaintext body, so messages written before capture began still load. A stale
// or unopenable shadow fails the read: it means capture or the data is broken,
// and plaintext must not paper over that. Rows whose body lives only in a
// content asset get the same treatment from the asset's encrypted copy.
func openShadowRows[T any](ctx context.Context, s *Service, authCtx *contextvalues.AuthContext, rows []T, body func(*T) messageBody) error {
	if s.transcriptShadow == nil || authCtx == nil || authCtx.ProjectID == nil || len(rows) == 0 {
		return nil
	}
	if _, enabled := s.transcriptShadowReadProjects[*authCtx.ProjectID]; !enabled {
		return nil
	}

	bodies := make([]messageBody, len(rows))
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		bodies[i] = body(&rows[i])
		ids[i] = bodies[i].id
	}

	opened, err := s.transcriptShadow.LoadMany(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, ids)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for _, b := range bodies {
		message, ok := opened[b.id]
		if !ok {
			continue
		}
		*b.content = message.Content
		*b.contentRaw = message.ContentRaw
		*b.toolCalls = message.ToolCalls
	}
	return openSealedContentAssets(ctx, s, authCtx.ActiveOrganizationID, *authCtx.ProjectID, bodies)
}

// openSealedContentAssets fills in the body of each message that has no inline
// content_raw from the encrypted copy of its content asset. Without one the
// message keeps reading the plaintext object. Each distinct object is opened
// once, in parallel.
func openSealedContentAssets(ctx context.Context, s *Service, organizationID string, projectID uuid.UUID, bodies []messageBody) error {
	if s.sealedAssets == nil {
		return nil
	}
	type pending struct {
		objectURL *url.URL
		identity  transcriptcrypto.AssetIdentity
		bodies    []messageBody
	}
	byURL := make(map[string]*pending)
	for _, b := range bodies {
		if len(*b.contentRaw) > 0 || !b.contentAssetURL.Valid || b.contentAssetURL.String == "" {
			continue
		}
		if p, ok := byURL[b.contentAssetURL.String]; ok {
			p.bodies = append(p.bodies, b)
			continue
		}
		objectURL, err := url.Parse(b.contentAssetURL.String)
		if err != nil {
			// loadMessageContentFields logs and degrades unparseable URLs.
			continue
		}
		hash, ok := transcriptstore.AssetContentHash(objectURL)
		if !ok {
			// The writer only seals content-addressed objects; any copy
			// next to another object cannot be verified.
			if err := s.sealedAssets.RefuseUnverifiable(ctx, objectURL); err != nil {
				return fmt.Errorf("open encrypted chat content asset: %w", err)
			}
			continue
		}
		byURL[b.contentAssetURL.String] = &pending{
			objectURL: objectURL,
			identity:  transcriptcrypto.AssetIdentity{Kind: transcriptcrypto.AssetMessageContent, ProjectID: projectID, ChatID: b.chatID, SHA256: hash},
			bodies:    []messageBody{b},
		}
	}

	var group errgroup.Group
	group.SetLimit(maxConcurrentChatAssetWork)
	for _, p := range byURL {
		group.Go(func() error {
			body, found, err := s.sealedAssets.Open(ctx, organizationID, p.identity, p.objectURL, MaxAssetReadSize)
			if err != nil {
				return fmt.Errorf("open encrypted chat content asset: %w", err)
			}
			if !found {
				return nil
			}
			for _, b := range p.bodies {
				*b.contentRaw = body
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("open encrypted chat content assets: %w", err)
	}
	return nil
}

// readContentPart returns a content part's body. For a shadow read project it
// comes from the object's encrypted copy when one exists; an encrypted copy
// that does not open fails the read. Otherwise it is the plaintext object.
func (s *Service) readContentPart(ctx context.Context, authCtx *contextvalues.AuthContext, chatID, contentPartID uuid.UUID, contentAssetURL string) (string, error) {
	if s.sealedAssets == nil || authCtx == nil || authCtx.ProjectID == nil {
		return s.loadContentPartContent(ctx, chatID, contentPartID, contentAssetURL), nil
	}
	if _, enabled := s.transcriptShadowReadProjects[*authCtx.ProjectID]; !enabled {
		return s.loadContentPartContent(ctx, chatID, contentPartID, contentAssetURL), nil
	}
	objectURL, err := url.Parse(contentAssetURL)
	if err != nil {
		return s.loadContentPartContent(ctx, chatID, contentPartID, contentAssetURL), nil
	}
	hash, ok := transcriptstore.AssetContentHash(objectURL)
	if !ok {
		// The writer only seals content-addressed objects; any copy next to
		// another object cannot be verified.
		if err := s.sealedAssets.RefuseUnverifiable(ctx, objectURL); err != nil {
			return "", fmt.Errorf("open encrypted chat content part: %w", err)
		}
		return s.loadContentPartContent(ctx, chatID, contentPartID, contentAssetURL), nil
	}
	identity := transcriptcrypto.AssetIdentity{Kind: transcriptcrypto.AssetContentPart, ProjectID: *authCtx.ProjectID, ChatID: chatID, SHA256: hash}
	body, found, err := s.sealedAssets.Open(ctx, authCtx.ActiveOrganizationID, identity, objectURL, MaxAssetReadSize)
	if err != nil {
		return "", fmt.Errorf("open encrypted chat content part: %w", err)
	}
	if !found {
		return s.loadContentPartContent(ctx, chatID, contentPartID, contentAssetURL), nil
	}
	return string(body), nil
}
