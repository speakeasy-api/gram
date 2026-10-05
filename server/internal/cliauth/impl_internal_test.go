package cliauth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/hooksacting"
)

func TestMintRefreshOrRevokeCleansUpFailedEnrollment(t *testing.T) {
	t.Parallel()
	mintErr := errors.New("mint failed")
	revoked := false

	refreshToken, err := mintRefreshOrRevoke(
		func() (string, error) { return "", mintErr },
		func() error {
			revoked = true
			return nil
		},
	)

	require.Empty(t, refreshToken)
	require.ErrorIs(t, err, mintErr)
	require.True(t, revoked)
}

func TestMintAssertionAfterIdentityChecksRemintsCandidate(t *testing.T) {
	t.Parallel()

	events := make([]string, 0, 4)
	mintCalls := 0
	mint := func(hooksacting.RefreshIdentity, delegation.MintRequest) (string, error) {
		events = append(events, "mint")
		mintCalls++
		if mintCalls == 1 {
			return "expired-during-membership-check", nil
		}
		return "fresh-after-membership-check", nil
	}
	enrollment := func() (bool, error) {
		events = append(events, "enrollment")
		return true, nil
	}
	membership := func() (bool, error) {
		events = append(events, "membership")
		return true, nil
	}

	assertion, err := mintAssertionAfterIdentityChecks(hooksacting.RefreshIdentity{}, delegation.MintRequest{}, mint, enrollment, membership)

	require.NoError(t, err)
	require.Equal(t, "fresh-after-membership-check", assertion)
	require.Equal(t, []string{"mint", "enrollment", "membership", "mint"}, events)
}
