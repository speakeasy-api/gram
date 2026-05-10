// Package memory implements the gram://memory/* platform tools that wrap the
// MemoryService for use by assistant agents at runtime. These tools are gated
// on the FeatureAssistantMemory product flag by the platformtools registry.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/memory"
)

// Service is the surface of the assistant memory service that the platform
// memory tools use. *memory.MemoryService satisfies it; tests substitute a
// fake.
type Service interface {
	Remember(ctx context.Context, assistantID uuid.UUID, projectID uuid.UUID, organizationID string, content string, tags []string) (memory.RememberResult, error)
	Recall(ctx context.Context, assistantID uuid.UUID, organizationID string, query string, limit int, tags []string) ([]memory.RecallResult, error)
	Forget(ctx context.Context, assistantID uuid.UUID, projectID uuid.UUID, organizationID string, query string, tags []string) (memory.ForgetResult, error)
}

const (
	handlerRemember = "remember"
	handlerRecall   = "recall"
	handlerForget   = "forget"
)

// readPayload reads and JSON-decodes the request body into target.
func readPayload(payload io.Reader, target any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

// writeJSON encodes value as JSON to wr.
func writeJSON(wr io.Writer, value any) error {
	if err := json.NewEncoder(wr).Encode(value); err != nil {
		return fmt.Errorf("encode response body: %w", err)
	}
	return nil
}

func memoryToolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
