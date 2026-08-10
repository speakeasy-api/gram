package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthnChallengeState_MintOriginOr_PrefersSnapshot(t *testing.T) {
	t.Parallel()

	state := AuthnChallengeState{Endpoint: EndpointRef{BaseURL: "https://custom.example.com"}}
	require.Equal(t, "https://custom.example.com", state.mintOriginOr("https://app.example.com"))
}

// An empty snapshot is the one case where the true mint origin is
// unrecoverable and the caller's fallback is all there is, so pin the choice
// explicitly rather than letting a change alter it unnoticed.
func TestAuthnChallengeState_MintOriginOr_EmptySnapshotUsesFallback(t *testing.T) {
	t.Parallel()

	state := AuthnChallengeState{Endpoint: EndpointRef{BaseURL: ""}}
	require.Equal(t, "https://app.example.com", state.mintOriginOr("https://app.example.com"))
}
