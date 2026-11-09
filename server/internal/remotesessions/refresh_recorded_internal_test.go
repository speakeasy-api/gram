package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A collapsed refresh error keeps which row its refresh already wrote.
func TestWithRecordedRefreshKeepsWhatTheRefreshWrote(t *testing.T) {
	t.Parallel()

	step := withRecordedRefresh(ErrNoValidToken, UpstreamToken{refreshStepRecorded: true})
	require.ErrorIs(t, step, ErrNoValidToken)
	require.True(t, RefreshStepRecorded(step))
	require.False(t, RefreshFailureRecorded(step))

	standalone := withRecordedRefresh(ErrRemoteSessionMisconfigured, UpstreamToken{refreshFailureRecorded: true})
	require.ErrorIs(t, standalone, ErrRemoteSessionMisconfigured)
	require.True(t, RefreshFailureRecorded(standalone))
	require.False(t, RefreshStepRecorded(standalone))

	require.Equal(t, ErrNoValidToken, withRecordedRefresh(ErrNoValidToken, UpstreamToken{}))
}
