package embedding

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFingerprint_SameInputsSameHash(t *testing.T) {
	h1 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	h2 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	require.Equal(t, h1, h2)
	require.NotEmpty(t, h1)
}

func TestFingerprint_DifferentContentDifferentHash(t *testing.T) {
	h1 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	h2 := Fingerprint("goodbye world", "h2", `{"dept":"eng"}`, "abc123")
	require.NotEqual(t, h1, h2)
}

func TestFingerprint_DifferentStrategyDifferentHash(t *testing.T) {
	h1 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	h2 := Fingerprint("hello world", "h3", `{"dept":"eng"}`, "abc123")
	require.NotEqual(t, h1, h2)
}

func TestFingerprint_DifferentMetadataDifferentHash(t *testing.T) {
	h1 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	h2 := Fingerprint("hello world", "h2", `{"dept":"sales"}`, "abc123")
	require.NotEqual(t, h1, h2)
}

func TestFingerprint_DifferentManifestDifferentHash(t *testing.T) {
	h1 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "abc123")
	h2 := Fingerprint("hello world", "h2", `{"dept":"eng"}`, "def456")
	require.NotEqual(t, h1, h2)
}
