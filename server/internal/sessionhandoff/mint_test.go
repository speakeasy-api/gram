package sessionhandoff

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSessionHandoffMinterRequiresHTTPSServerURL(t *testing.T) {
	t.Parallel()
	pool := &pgxpool.Pool{}
	blobs := assetstest.NewTestBlobStore(t)
	auditor := audit.NewLogger()
	require.True(t, NewSessionHandoffMinter(testenv.NewLogger(t), pool, blobs, "https://app.example.test", auditor).Valid())
	for _, raw := range []string{"http://app.example.test", "not a URL", "https:///missing-host"} {
		require.False(t, NewSessionHandoffMinter(testenv.NewLogger(t), pool, blobs, raw, auditor).Valid(), raw)
	}
}

func TestValidHTTPSBaseURL(t *testing.T) {
	t.Parallel()
	accepted, ok := validHTTPSBaseURL("https://app.example.test/base")
	require.True(t, ok)
	require.Equal(t, "https", accepted.Scheme)
	require.Equal(t, "app.example.test", accepted.Host)

	for _, raw := range []string{"http://app.example.test", "not a URL", "https:///missing-host", "https://user@example.test", "https://app.example.test?token=value"} {
		parsed, valid := validHTTPSBaseURL(raw)
		require.False(t, valid, raw)
		require.Nil(t, parsed, raw)
	}
}

func TestPublicHandoffTokenContract(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/shared/handoffs/", PublicRoutePrefix)
	require.Equal(t, PublicRoutePrefix+"{token}", PublicRoutePattern)
	require.True(t, ValidToken("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	require.False(t, ValidToken("short"))
	require.False(t, ValidToken("zz23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
}

func TestClampHandoffTTL(t *testing.T) {
	t.Parallel()
	below, above, exact := 1, 7200, 300
	require.Equal(t, 15*time.Minute, ClampHandoffTTL(nil))
	require.Equal(t, time.Minute, ClampHandoffTTL(&below))
	require.Equal(t, time.Hour, ClampHandoffTTL(&above))
	require.Equal(t, 5*time.Minute, ClampHandoffTTL(&exact))
}
