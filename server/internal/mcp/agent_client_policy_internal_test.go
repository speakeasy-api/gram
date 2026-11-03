package mcp

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAgentClientMintRejectsLifetimeOverrides(t *testing.T) {
	now := time.Now()
	duration := time.Hour
	for _, tc := range []struct {
		name   string
		mutate func(*mintSessionParams)
	}{
		{"authorization deadline", func(p *mintSessionParams) { p.AuthorizationExpiresAt = &now }},
		{"desired duration", func(p *mintSessionParams) { p.DesiredSessionDuration = &duration }},
		{"replayable", func(p *mintSessionParams) { p.Replayable = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := mintSessionParams{Policy: sessionIssuancePolicyAgentClient, Audience: "https://example.com/mcp/test", Subject: urn.NewAgentSubject(uuid.New())}
			tc.mutate(&params)
			_, err := (&Service{}).mintSession(t.Context(), &ResolvedMcpEndpoint{}, nil, nil, params, slog.Default())
			var shared *oops.ShareableError
			require.ErrorAs(t, err, &shared)
			require.Equal(t, oops.CodeUnexpected, shared.Code)
			require.Contains(t, shared.Error(), "invalid agent-client session issuance parameters")
		})
	}
}
