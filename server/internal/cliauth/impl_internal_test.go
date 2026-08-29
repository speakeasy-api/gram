package cliauth

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/hooksacting"
)

func TestMintAssertionAfterMembershipRemintsCandidate(t *testing.T) {
	t.Parallel()

	events := make([]string, 0, 3)
	mintCalls := 0
	mint := func(hooksacting.RefreshIdentity, delegation.MintRequest) (string, error) {
		events = append(events, "mint")
		mintCalls++
		if mintCalls == 1 {
			return "expired-during-membership-check", nil
		}
		return "fresh-after-membership-check", nil
	}
	membership := func() (bool, error) {
		events = append(events, "membership")
		return true, nil
	}

	assertion, err := mintAssertionAfterMembership(hooksacting.RefreshIdentity{}, delegation.MintRequest{}, mint, membership)

	require.NoError(t, err)
	require.Equal(t, "fresh-after-membership-check", assertion)
	require.Equal(t, []string{"mint", "membership", "mint"}, events)
}
