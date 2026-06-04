package users

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// OrganizationsService is the subset of the organizations management service
// that the managed assistant's user-directory tools call. The concrete
// organizations service satisfies it; tools pass nil auth tokens because the
// assistant runtime supplies auth context out of band.
type OrganizationsService interface {
	ListUsers(ctx context.Context, payload *organizations.ListUsersPayload) (*organizations.ListUsersResult, error)
}

func readOnlyToolAnnotations() *types.ToolAnnotations {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func decodeToolInput(payload io.Reader, dst any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func encodeToolResult(wr io.Writer, result any) error {
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
