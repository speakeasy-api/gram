package mcp

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/threadcontext"
)

func (s *Service) resolvePlatformThreadContext(ctx context.Context, threadID uuid.UUID, write bool) (contextvalues.AssistantPrincipal, string, error) {
	principal, chatID, err := threadcontext.Resolve(ctx, s.db, s.authz, threadID, write)
	if err != nil {
		return principal, chatID, fmt.Errorf("resolve platform thread: %w", err)
	}
	return principal, chatID, nil
}
