package hostedmcpbackfill

import (
	"context"
	"errors"
	"io"
	"net"
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
		{"unauthorized", oops.E(oops.CodeUnauthorized, nil, "missing hosted MCP actor"), "", false},
		{"plain error", errors.New("boom"), "", false},
		{"deadlock", oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.DeadlockDetected}, "create hosted MCP endpoint"), "", false},
		{"serialization failure", oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.SerializationFailure}, "create hosted MCP endpoint"), "", false},
		{"lock timeout", oops.E(oops.CodeInvalid, &pgconn.PgError{Code: pgerrcode.LockNotAvailable}, "lock hosted MCP server"), "", false},
		{"statement timeout", oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.QueryCanceled}, "update hosted MCP server"), "", false},
		{"idle timeout", oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.IdleInTransactionSessionTimeout}, "update hosted MCP server"), "", false},
		{"context canceled", oops.E(oops.CodeConflict, context.Canceled, "update hosted MCP server"), "", false},
		{"connection reset", oops.E(oops.CodeConflict, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}, "create hosted MCP server"), "", false},
		{"unexpected eof", oops.E(oops.CodeConflict, io.ErrUnexpectedEOF, "create hosted MCP endpoint"), "", false},
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

func TestSyncErrorReasonNamesTheViolatedConstraint(t *testing.T) {
	t.Parallel()

	err := oops.E(oops.CodeConflict, &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "mcp_endpoints_slug_key"}, "create hosted MCP endpoint")
	require.Equal(t, "create hosted MCP endpoint (constraint mcp_endpoints_slug_key)", syncErrorReason(err))
	require.Equal(t, "hosted MCP address is already in use", syncErrorReason(oops.E(oops.CodeConflict, hostedmcp.ErrAddressInUse, "hosted MCP address is already in use")))
}
