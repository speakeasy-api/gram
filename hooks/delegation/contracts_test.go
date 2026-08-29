package delegation_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
)

func TestProofOfPossessionBindsEveryInvocationField(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	req := delegation.MintRequest{RefreshToken: "refresh", ContractVersion: delegation.ContractVersion, Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse, SessionID: "session", IdempotencyKey: "invocation", SignedAt: 123, Nonce: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	req.Signature, err = delegation.Sign(privateKey, req)
	require.NoError(t, err)
	require.NoError(t, delegation.Verify(publicKey, req))

	mutations := []func(*delegation.MintRequest){
		func(v *delegation.MintRequest) { v.RefreshToken = "other" },
		func(v *delegation.MintRequest) { v.Provider = delegation.ProviderCodex },
		func(v *delegation.MintRequest) { v.Event = delegation.EventUserPromptSubmit },
		func(v *delegation.MintRequest) { v.SessionID = "other" },
		func(v *delegation.MintRequest) { v.IdempotencyKey = "other" },
		func(v *delegation.MintRequest) { v.SignedAt++ },
		func(v *delegation.MintRequest) { v.Nonce = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB" },
	}
	for _, mutate := range mutations {
		changed := req
		mutate(&changed)
		require.Error(t, delegation.Verify(publicKey, changed))
	}
}

func TestNewNonceIsRandomAndCanonical(t *testing.T) {
	first, err := delegation.NewNonce()
	require.NoError(t, err)
	second, err := delegation.NewNonce()
	require.NoError(t, err)
	require.Len(t, first, 43)
	require.Len(t, second, 43)
	require.NotEqual(t, first, second)
}

func TestApprovedCoverageIsExact(t *testing.T) {
	for _, provider := range []string{delegation.ProviderClaude, delegation.ProviderCodex} {
		require.True(t, delegation.Approved(provider, delegation.EventUserPromptSubmit))
		require.True(t, delegation.Approved(provider, delegation.EventPreToolUse))
		require.False(t, delegation.Approved(provider, "PermissionRequest"))
	}
	require.False(t, delegation.Approved("cursor", delegation.EventPreToolUse))
	bindings := delegation.ApprovedBindings()
	require.Len(t, bindings, 4)
	bindings[0].Provider = "mutated"
	require.NotEqual(t, "mutated", delegation.ApprovedBindings()[0].Provider, "enumeration must return a copy")
}
