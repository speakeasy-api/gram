package xaareadiness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

func TestDerive(t *testing.T) {
	t.Parallel()

	ready := Inputs{AdvertisesIDJAG: true, AgentRecorded: true, Confirmed: true}
	with := func(mutate func(*Inputs)) Inputs {
		in := ready
		mutate(&in)
		return in
	}

	tests := []struct {
		name string
		in   Inputs
		want State
	}{
		{"no id-jag is not applicable", with(func(i *Inputs) { i.AdvertisesIDJAG = false }), StateNotApplicable},
		{"agent first", with(func(i *Inputs) { i.AgentRecorded = false; i.Confirmed = false }), StateNeedsAgent},
		{"unconfirmed connection", with(func(i *Inputs) { i.Confirmed = false }), StateNeedsConnection},
		{"confirmation is connected", ready, StateConnected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Derive(tt.in))
		})
	}
	require.Equal(t, ReasonNoIDJAG, NotApplicableReason(with(func(i *Inputs) { i.AdvertisesIDJAG = false })))
	require.Empty(t, NotApplicableReason(ready))
}

func TestStatePending(t *testing.T) {
	t.Parallel()

	for _, s := range []State{StateNeedsAgent, StateNeedsConnection} {
		require.True(t, s.Pending(), string(s))
	}
	for _, s := range []State{StateNotApplicable, StateConnected} {
		require.False(t, s.Pending(), string(s))
	}
}

func TestAdvertisesIDJAG(t *testing.T) {
	t.Parallel()

	require.True(t, AdvertisesIDJAG([]string{oauthwire.GrantTypeJWTBearer}, []string{oauthwire.GrantProfileIDJAG}))
	require.False(t, AdvertisesIDJAG([]string{oauthwire.GrantTypeJWTBearer}, nil), "jwt-bearer alone covers other profiles")
	require.False(t, AdvertisesIDJAG(nil, []string{oauthwire.GrantProfileIDJAG}), "the profile needs the grant type")
	require.False(t, AdvertisesIDJAG(nil, nil))
}

func TestNormalizeAudience(t *testing.T) {
	t.Parallel()

	got, err := normalizeAudience("  https://auth.linear.com ")
	require.NoError(t, err)
	require.Equal(t, "https://auth.linear.com", got)
	got, err = normalizeAudience("https://auth.example.com/oauth2/default")
	require.NoError(t, err)
	require.Equal(t, "https://auth.example.com/oauth2/default", got)
	for _, bad := range []string{"", "auth.linear.com", "http://auth.linear.com", "https://auth.linear.com/?x=1", "https://auth.linear.com/#f", "https://auth.linear.com/?", "https://auth.linear.com#", "https://user@auth.linear.com"} {
		_, err := normalizeAudience(bad)
		require.Error(t, err, bad)
	}
}

func TestNormalizeAudienceLength(t *testing.T) {
	t.Parallel()
	const prefix = "https://auth.example.com/"
	for _, tt := range []struct {
		name      string
		raw       string
		wantError bool
	}{
		{name: "ASCII boundary", raw: prefix + strings.Repeat("a", 512-len(prefix))},
		{name: "ASCII oversized", raw: prefix + strings.Repeat("a", 513-len(prefix)), wantError: true},
		{name: "multibyte boundary", raw: prefix + strings.Repeat("界", 512-len(prefix))},
		{name: "multibyte oversized", raw: prefix + strings.Repeat("界", 513-len(prefix)), wantError: true},
		{name: "whitespace boundary", raw: " " + prefix + strings.Repeat("\u2003", 511-len(prefix))},
		{name: "oversized whitespace padding", raw: " " + prefix + strings.Repeat("\u2003", 512-len(prefix)), wantError: true},
		{name: "oversized whitespace only", raw: strings.Repeat(" ", 513), wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeAudience(tt.raw)
			if tt.wantError {
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, strings.TrimSpace(tt.raw), got)
		})
	}
}
