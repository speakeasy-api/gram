package guardian

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
