package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/cli/internal/profile"
)

func TestDoWhoami_NoProfileIsNotAuthenticated(t *testing.T) {
	t.Parallel()

	_, err := DoWhoami(t.Context(), WhoamiOptions{Profile: nil, APIKey: "", APIURL: ""})

	require.ErrorIs(t, err, ErrNotAuthenticated)
	require.Contains(t, whoamiError(err).Error(), "speakeasy auth")
}

func TestDoWhoami_NoAPIKeyIsNotAuthenticated(t *testing.T) {
	t.Parallel()

	_, err := DoWhoami(t.Context(), WhoamiOptions{Profile: &profile.Profile{Name: "default", Secret: "", DefaultProjectSlug: "", APIUrl: "", Org: nil, Projects: nil}, APIKey: "", APIURL: ""})

	require.ErrorIs(t, err, ErrNotAuthenticated)
	require.Contains(t, whoamiError(err).Error(), "speakeasy auth")
}

func TestDoWhoami_InvalidAPIURLHasNoAuthHint(t *testing.T) {
	t.Parallel()

	_, err := DoWhoami(t.Context(), WhoamiOptions{Profile: &profile.Profile{Name: "default", Secret: "", DefaultProjectSlug: "", APIUrl: "", Org: nil, Projects: nil}, APIKey: "key", APIURL: "http://[::1"})

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotAuthenticated)
	require.NotContains(t, whoamiError(err).Error(), "speakeasy auth")
}

func TestWhoamiError_PassesOtherErrorsThrough(t *testing.T) {
	t.Parallel()

	verifyErr := errors.New("failed to verify API key: connection refused")

	require.Same(t, verifyErr, whoamiError(verifyErr))
}
