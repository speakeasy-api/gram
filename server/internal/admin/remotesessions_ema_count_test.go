package admin

import (
	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIssuerConversionsPreserveEMABindingCounts(t *testing.T) {
	t.Parallel()
	var detail adminrsgen.GlobalRemoteSessionIssuer
	detail.EmaBindingCount = new(3)
	require.Equal(t, 3, *convertGlobalRemoteSessionIssuer(&detail).EmaBindingCount)
	detail.EmaBindingCount = nil
	require.Nil(t, convertGlobalRemoteSessionIssuer(&detail).EmaBindingCount)
	var preflight adminrsgen.IssuerMigratePreflight
	preflight.EmaBindingCount = 4
	require.Equal(t, 4, convertIssuerMigratePreflight(&preflight).EmaBindingCount)
}
