package assistantmemories

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/assistant_memories"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/memory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// requireMemoryAccess validates auth context, gates on the assistant memory
// feature flag, and runs the requested authz check. Returns the resolved
// AuthContext on success.
func (s *Service) requireMemoryAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureAssistantMemory)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check assistant memory feature").Log(ctx, s.logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "assistant memory feature is not enabled for this organization")
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	return authCtx, nil
}

func (s *Service) ListAssistantMemories(ctx context.Context, payload *gen.ListAssistantMemoriesPayload) (*gen.ListAssistantMemoriesResult, error) {
	authCtx, err := s.requireMemoryAccess(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	assistantID, err := uuid.Parse(payload.AssistantID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").Log(ctx, s.logger)
	}

	cursorCreatedAt, cursorID, err := decodeListCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}

	limit := conv.SafeInt32(payload.Limit)

	result, err := s.memory.List(ctx, *authCtx.ProjectID, memory.ListParams{
		AssistantID:     assistantID,
		Tags:            payload.Tags,
		IncludeDeleted:  payload.IncludeDeleted,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		Limit:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list assistant memories: %w", err)
	}

	memories := make([]*types.AssistantMemory, 0, len(result.Memories))
	for _, m := range result.Memories {
		memories = append(memories, memoryToView(repo.GetAssistantMemoryByIDRow(m)))
	}

	var nextCursor *string
	if len(result.Memories) == int(limit) && limit > 0 {
		last := result.Memories[len(result.Memories)-1]
		if last.CreatedAt.Valid {
			encoded := encodeListCursor(last.CreatedAt.Time, last.ID)
			nextCursor = &encoded
		}
	}

	return &gen.ListAssistantMemoriesResult{
		Memories:   memories,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetAssistantMemory(ctx context.Context, payload *gen.GetAssistantMemoryPayload) (*types.AssistantMemory, error) {
	authCtx, err := s.requireMemoryAccess(ctx, authz.ScopeProjectRead)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid memory id").Log(ctx, s.logger)
	}

	mem, err := s.memory.Get(ctx, *authCtx.ProjectID, id)
	if err != nil {
		return nil, fmt.Errorf("get assistant memory: %w", err)
	}

	return memoryToView(mem), nil
}

func (s *Service) DeleteAssistantMemory(ctx context.Context, payload *gen.DeleteAssistantMemoryPayload) error {
	authCtx, err := s.requireMemoryAccess(ctx, authz.ScopeProjectWrite)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid memory id").Log(ctx, s.logger)
	}

	if err := s.memory.DeleteByID(ctx, *authCtx.ProjectID, id); err != nil {
		return fmt.Errorf("delete assistant memory: %w", err)
	}

	return nil
}

// encodeListCursor produces a URL-safe base64 cursor of the form
// "<rfc3339nano>|<uuid>" for keyset-based pagination over (created_at, id).
func encodeListCursor(createdAt time.Time, id uuid.UUID) string {
	payload := fmt.Sprintf("%s|%s", createdAt.UTC().Format(time.RFC3339Nano), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeListCursor returns the (created_at, id) pair encoded by encodeListCursor.
// A nil pointer or empty string yields (nil, nil, nil) so callers can distinguish
// "no cursor" from "invalid cursor".
func decodeListCursor(cursor *string) (*time.Time, *uuid.UUID, error) {
	if cursor == nil || *cursor == "" {
		return nil, nil, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return nil, nil, fmt.Errorf("decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid cursor format")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("parse cursor timestamp: %w", err)
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("parse cursor id: %w", err)
	}

	return &t, &id, nil
}
