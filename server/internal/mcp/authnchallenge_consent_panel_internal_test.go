package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The panel describes a live grant; a card that says reconnect keeps only which account it was.
func TestRemoteSessionCardPanel(t *testing.T) {
	t.Parallel()

	full := remoteSessionCard{ConnectedAs: "Grant Owner · grant-owner@example.com", AccountChips: []string{"Acme Docs", "octocat"}, TokenActive: true, ValidationReason: "linear did not answer in time", RefreshExpiresIn: "29 days", AuthorizationExpiresIn: "365 days"}
	with := func(mutate func(c *remoteSessionCard)) remoteSessionCard {
		c := full
		mutate(&c)
		return c
	}
	cases := []struct {
		name string
		card remoteSessionCard
		want cardPanel
	}{
		{name: "not connected", card: with(func(c *remoteSessionCard) {}), want: cardPanel{}},
		{name: "connected and verified", card: with(func(c *remoteSessionCard) { c.Connected, c.Verified = true, true }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowAccountContext: true, ShowToken: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "chips without identity", card: with(func(c *remoteSessionCard) { c.Connected, c.Verified, c.ConnectedAs = true, true, "" }), want: cardPanel{GrantLive: true, ShowAccountContext: true, ShowToken: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "chips without identity on a reconnect card", card: with(func(c *remoteSessionCard) { c.Connected, c.Rejected, c.ConnectedAs = true, true, "" }), want: cardPanel{ShowAccountContext: true, ShowDetails: true}},
		{name: "identity without chips", card: with(func(c *remoteSessionCard) { c.Connected, c.Verified, c.AccountChips = true, true, nil }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowToken: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "connected never validated", card: with(func(c *remoteSessionCard) { c.Connected = true; c.ValidationReason = "" }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowAccountContext: true, ShowToken: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "connected unknown verdict", card: with(func(c *remoteSessionCard) { c.Connected, c.Unverified = true, true }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowAccountContext: true, ShowToken: true, ShowValidationReason: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "connected unknown verdict without introspection", card: with(func(c *remoteSessionCard) { c.Connected, c.Unverified, c.TokenActive = true, true, false }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowAccountContext: true, ShowValidationReason: true, ShowLapse: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "auto refresh on replaces the lapse", card: with(func(c *remoteSessionCard) { c.Connected, c.Verified, c.AutoRefreshChecked = true, true, true }), want: cardPanel{GrantLive: true, ShowIdentity: true, ShowAccountContext: true, ShowToken: true, ShowAutoRefresh: true, ShowAccessEnd: true, ShowDetails: true}},
		{name: "auto refresh on a reconnect card says nothing", card: with(func(c *remoteSessionCard) { c.Connected, c.Rejected, c.AutoRefreshChecked = true, true, true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "rejected", card: with(func(c *remoteSessionCard) { c.Connected, c.Rejected = true, true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "inactive", card: with(func(c *remoteSessionCard) { c.Connected, c.Inactive = true, true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "identity reconnect", card: with(func(c *remoteSessionCard) { c.Connected, c.IdentityReconnect = true, true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "expired", card: with(func(c *remoteSessionCard) { c.Expired = true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "unroutable", card: with(func(c *remoteSessionCard) { c.Unroutable = true }), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowDetails: true}},
		{name: "rejected without an account or chips", card: with(func(c *remoteSessionCard) {
			c.Connected, c.Rejected, c.ConnectedAs, c.AccountChips = true, true, "", nil
		}), want: cardPanel{}},
		{name: "connected with nothing to say", card: remoteSessionCard{Connected: true}, want: cardPanel{GrantLive: true}},
		{name: "links only, not connected", card: remoteSessionCard{IssuerDocumentationURL: "https://docs.example.com"}, want: cardPanel{ShowLinks: true, ShowDetails: true}},
		{name: "links only, connected", card: remoteSessionCard{Connected: true, IssuerTosURL: "https://example.com/tos"}, want: cardPanel{GrantLive: true, ShowLinks: true, ShowDetails: true}},
		{name: "inactive with links", card: with(func(c *remoteSessionCard) {
			c.Connected, c.Inactive, c.IssuerPolicyURL = true, true, "https://example.com/policy"
		}), want: cardPanel{ShowIdentity: true, ShowAccountContext: true, ShowLinks: true, ShowDetails: true}},
		{name: "rejected without an account, with links", card: with(func(c *remoteSessionCard) {
			c.Connected, c.Rejected, c.ConnectedAs, c.IssuerDocumentationURL = true, true, "", "https://docs.example.com"
		}), want: cardPanel{ShowAccountContext: true, ShowLinks: true, ShowDetails: true}},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.card.Panel(), tc.name)
	}
}
