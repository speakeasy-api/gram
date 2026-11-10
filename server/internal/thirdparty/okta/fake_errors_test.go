package okta

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFake_SetMethodErrorZeroValue(t *testing.T) {
	t.Parallel()
	var fake Fake
	want := errors.New("denied")
	fake.SetMethodError("ListApps", nil)
	fake.SetMethodError("ListApps", want)
	_, err := fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 1, MaxPages: 0})
	require.ErrorIs(t, err, want)
	fake.SetMethodError("ListApps", nil)
	_, err = fake.ListApps(t.Context(), ListAppsRequest{Query: "", Status: "", Limit: 1, MaxPages: 0})
	require.NoError(t, err)
}
