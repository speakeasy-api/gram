package guardian

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithCheckRedirect(t *testing.T) {
	t.Parallel()

	var opts httpClientOptions
	require.Nil(t, opts.checkRedirect, "unset must keep net/http's default redirect policy")

	WithCheckRedirect(func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })(&opts)
	require.NotNil(t, opts.checkRedirect)
	require.ErrorIs(t, opts.checkRedirect(nil, nil), http.ErrUseLastResponse)
}

func TestWithDialTimeout(t *testing.T) {
	t.Parallel()

	var opts httpClientOptions
	require.Nil(t, opts.dialTimeout, "unset must leave the policy dialer default in place")

	WithDialTimeout(3 * time.Second)(&opts)
	require.NotNil(t, opts.dialTimeout)
	require.Equal(t, 3*time.Second, *opts.dialTimeout)

	WithDialTimeout(0)(&opts)
	require.NotNil(t, opts.dialTimeout, "explicit zero must be honored, not treated as unset")
	require.Equal(t, time.Duration(0), *opts.dialTimeout)

	WithDialTimeout(-time.Second)(&opts)
	require.NotNil(t, opts.dialTimeout)
	require.Equal(t, time.Duration(0), *opts.dialTimeout, "negative must normalize to disabled, not an expired deadline")
}
