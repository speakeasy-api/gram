package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenResponseRefreshTokenTimeoutSeconds(t *testing.T) {
	t.Parallel()

	seconds, reported := (tokenResponse{}).RefreshTokenTimeoutSeconds()
	require.False(t, reported)
	require.Zero(t, seconds)

	seconds, reported = (tokenResponse{RefreshExpiresIn: 3600}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)

	seconds, reported = (tokenResponse{RefreshTokenExpiresIn: 7200}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 7200, seconds)

	seconds, reported = (tokenResponse{
		RefreshExpiresIn:      3600,
		RefreshTokenExpiresIn: 7200,
	}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)
}

func TestTokenResponseStandardExpirationFields(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	refreshTimeout := int64(3600)
	authorizationLifetime := int64(7200)

	seconds, reported := (tokenResponse{RefreshTokenTimeout: &zero}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.Zero(t, seconds)

	seconds, reported = (tokenResponse{
		RefreshTokenTimeout: &refreshTimeout,
		RefreshExpiresIn:    1800,
	}).RefreshTokenTimeoutSeconds()
	require.True(t, reported)
	require.EqualValues(t, 3600, seconds)

	seconds, reported = (tokenResponse{
		AuthorizationExpiresIn: &authorizationLifetime,
	}).AuthorizationLifetimeSeconds()
	require.True(t, reported)
	require.EqualValues(t, 7200, seconds)
}
