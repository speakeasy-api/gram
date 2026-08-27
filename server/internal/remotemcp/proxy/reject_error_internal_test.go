package proxy

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

// TestRejectCodeForOopsMirrorsMCPCode guards the proxy against drifting from the
// /mcp endpoint's NewErrorFromCause table: both surfaces must resolve an
// oops.ShareableError to the same JSON-RPC code. The named cases pin the codes
// that previously diverged; the loop pins the whole table to oops.Code.MCPCode.
func TestRejectCodeForOopsMirrorsMCPCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, -32003, rejectCodeForOops(oops.CodeForbidden), "forbidden must use the shared Forbidden code")
	require.Equal(t, -32001, rejectCodeForOops(oops.CodeUnauthorized), "unauthorized must use the shared Unauthorized code")
	require.Equal(t, -32002, rejectCodeForOops(oops.CodeNotFound), "not found must use the shared Resource-not-found code")

	for _, code := range []oops.Code{
		oops.CodeUnauthorized,
		oops.CodeForbidden,
		oops.CodeBadRequest,
		oops.CodeConflict,
		oops.CodeUnsupportedMedia,
		oops.CodeNotFound,
		oops.CodeInvalid,
		oops.CodeUnexpected,
		oops.CodeInferenceDisabled,
	} {
		require.Equalf(t, int(code.MCPCode()), rejectCodeForOops(code), "proxy code for %q must equal the hosted MCPCode", code)
	}
}
