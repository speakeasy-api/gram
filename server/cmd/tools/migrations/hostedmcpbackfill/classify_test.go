package hostedmcpbackfill

import (
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestClassifySyncError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		err     error
		outcome Outcome
		ok      bool
	}{
		{"address check", oops.E(oops.CodeConflict, hostedmcp.ErrAddressInUse, "hosted MCP address is already in use"), OutcomeBlockedSlugCollision, true},
		{"address unique violation", oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.UniqueViolation}, "create hosted MCP endpoint"), OutcomeBlockedSlugCollision, true},
		{"multiple endpoints", oops.E(oops.CodeConflict, nil, "hosted MCP has multiple endpoints; resolve them before editing the toolset"), OutcomeBlockedSyncRejected, true},
		{"identity conflict", oops.E(oops.CodeConflict, nil, "hosted MCP identity belongs to another server"), OutcomeBlockedSyncRejected, true},
		{"invalid", oops.E(oops.CodeInvalid, nil, "online private network ingress is required"), OutcomeBlockedSyncRejected, true},
		{"unexpected", oops.E(oops.CodeUnexpected, errors.New("db down"), "load hosted MCP server"), "", false},
		{"plain error", errors.New("boom"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outcome, ok := classifySyncError(tc.err)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.outcome, outcome)
		})
	}
}
