package agents

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
)

func TestDeriveLifecycle(t *testing.T) {
	t.Parallel()

	set := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	tests := []struct {
		name  string
		agent repo.Agent
		want  Lifecycle
	}{
		{name: "active", agent: repo.Agent{}, want: LifecycleActive},
		{name: "suspended", agent: repo.Agent{SuspendedAt: set}, want: LifecycleSuspended},
		{name: "revoked", agent: repo.Agent{RevokedAt: set}, want: LifecycleRevoked},
		{name: "revoked takes precedence over suspended", agent: repo.Agent{SuspendedAt: set, RevokedAt: set}, want: LifecycleRevoked},
		{name: "deleted takes precedence over every state", agent: repo.Agent{SuspendedAt: set, RevokedAt: set, DeletedAt: set}, want: LifecycleDeleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, DeriveLifecycle(tt.agent))
		})
	}
}
