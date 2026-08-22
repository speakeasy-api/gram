package platformmcp

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var probeReceiptTestTime = time.Unix(1_700_000_000, 0)

func TestProbeReceiptRoundTrip(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	receipt, err := codec.Decode(value, principal, probeReceiptTestTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp", receipt.NormalizedURL)
	require.Equal(t, "digest-1", receipt.ProbeDigest)
	require.Equal(t, principal.OrganizationID, receipt.OrganizationID)
	require.Equal(t, principal.ConnectionID, receipt.Binding)
	require.Equal(t, probeReceiptTestTime.Add(probeReceiptTTL).Unix(), receipt.ExpiresAt)
}

func TestProbeReceiptUsesStableKeyMaterial(t *testing.T) {
	t.Parallel()

	first, err := newProbeReceiptCodec("stable-key")
	require.NoError(t, err)
	second, err := newProbeReceiptCodec("stable-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := first.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)
	_, err = second.Decode(value, principal, probeReceiptTestTime)
	require.NoError(t, err)
}

func TestProbeReceiptRequiresKeyMaterial(t *testing.T) {
	t.Parallel()

	_, err := newProbeReceiptCodec("")
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
}

func TestProbeReceiptExpires(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	_, err = codec.Decode(value, principal, probeReceiptTestTime.Add(probeReceiptTTL).Add(-time.Second))
	require.NoError(t, err, "a receipt must redeem right up to its expiry")

	_, err = codec.Decode(value, principal, probeReceiptTestTime.Add(probeReceiptTTL))
	require.ErrorIs(t, err, ErrProbeReceiptExpired, "a receipt must be refused at its expiry instant")

	_, err = codec.Decode(value, principal, probeReceiptTestTime.Add(time.Hour))
	require.ErrorIs(t, err, ErrProbeReceiptExpired)
}

func TestProbeReceiptRejectsTampering(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	_, err = codec.Decode("", principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	_, err = codec.Decode("!!not-base64!!", principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	_, err = codec.Decode(value+"x", principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	_, err = codec.Decode(value[:len(value)/2], principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)

	// Flipping one payload byte while keeping the signature must fail
	// verification.
	token, err := base64.RawURLEncoding.DecodeString(value)
	require.NoError(t, err)
	token[0] ^= 0x01
	_, err = codec.Decode(base64.RawURLEncoding.EncodeToString(token), principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)

	// A receipt minted under a different key is not authentic here.
	other, err := newProbeReceiptCodec("other-receipt-key")
	require.NoError(t, err)
	foreign, err := other.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)
	_, err = codec.Decode(foreign, principal, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
}

func TestProbeReceiptRejectsDifferentConnection(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	other := principal
	other.ConnectionID = "connection-2"
	_, err = codec.Decode(value, other, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)

	// A different connection learns only that the receipt is not its own —
	// never whether it would still redeem — so the mismatch wins even once the
	// receipt has expired.
	_, err = codec.Decode(value, other, probeReceiptTestTime.Add(time.Hour))
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)

	// Unlike catalog cursors, a receipt binds to the connection rather than to
	// its generation: reauthorization within the TTL does not orphan it.
	reauthorized := principal
	reauthorized.Generation = "generation-2"
	_, err = codec.Decode(value, reauthorized, probeReceiptTestTime)
	require.NoError(t, err)
}

func TestProbeReceiptRejectsCrossOrganizationReuse(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	other := principal
	other.OrganizationID = "other-organization"
	_, err = codec.Decode(value, other, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)
}

// A receipt carries no project: the probe mutates nothing project-scoped, so
// the same connection may redeem one receipt against any project it is
// eligible for within the TTL. Project eligibility is the registration flow's
// own check, and redeeming is idempotent at the codec layer.
func TestProbeReceiptIsNotProjectBound(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	value, err := codec.Encode(principal, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)

	first, err := codec.Decode(value, principal, probeReceiptTestTime)
	require.NoError(t, err)
	second, err := codec.Decode(value, principal, probeReceiptTestTime)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// A connection-less caller has no connection ID to bind a receipt to. Binding
// to its surface and subject keeps the codec working should the assistant
// audience ever be admitted, and still refuses a receipt minted for a
// different caller.
func TestProbeReceiptBindsConnectionlessCallerToItsSubject(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	assistant := Principal{OrganizationID: "organization", UserID: "user-1", Surface: SurfaceProjectAssistant}
	require.False(t, assistant.HasConnection())
	require.NotEmpty(t, principalReceiptBinding(assistant))

	value, err := codec.Encode(assistant, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)
	_, err = codec.Decode(value, assistant, probeReceiptTestTime)
	require.NoError(t, err)

	other := assistant
	other.UserID = "user-2"
	_, err = codec.Decode(value, other, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)

	// An OAuth caller acting for the same user is a different caller: its
	// receipts bind to its connection, so neither direction can replay the
	// other's.
	connected := Principal{OrganizationID: "organization", UserID: "user-1", ConnectionID: "connection-1", Generation: "generation-1"}
	_, err = codec.Decode(value, connected, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)
	connectedValue, err := codec.Encode(connected, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)
	_, err = codec.Decode(connectedValue, assistant, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptContextMismatch)
}

// A principal claiming a connection through its generation alone has an
// incomplete identity, mirroring the operation budget's refusal of the same
// shape.
func TestProbeReceiptRefusesIncompleteConnectionIdentity(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	generationOnly := Principal{OrganizationID: "organization", UserID: "user-1", Generation: "generation-1"}
	require.True(t, generationOnly.HasConnection())
	require.Empty(t, principalReceiptBinding(generationOnly))

	_, err = codec.Encode(generationOnly, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)

	connected := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}
	value, err := codec.Encode(connected, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.NoError(t, err)
	_, err = codec.Decode(value, generationOnly, probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
}

// Encode refuses anything but an already-normalized, shape-valid URL, so a
// receipt can never smuggle a URL the probe's validation would have refused
// and the redeemer never has to re-validate.
func TestProbeReceiptEncodeRequiresNormalizedURL(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	for _, raw := range []string{
		"",
		"https://EXAMPLE.com/mcp",
		"https://example.com:443/mcp",
		"http://example.com/mcp",
		"https://user@example.com/mcp",
		"https://example.com/mcp#fragment",
	} {
		_, err := codec.Encode(principal, raw, "digest-1", probeReceiptTestTime)
		require.ErrorIs(t, err, ErrProbeReceiptInvalid, "url %q must be refused at mint time", raw)
	}
}

func TestProbeReceiptEncodeRequiresDigestAndClock(t *testing.T) {
	t.Parallel()

	codec, err := newProbeReceiptCodec("test-receipt-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection-1", Generation: "generation-1"}

	_, err = codec.Encode(principal, "https://example.com/mcp", "", probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	_, err = codec.Encode(principal, "https://example.com/mcp", "digest-1", time.Time{})
	require.ErrorIs(t, err, ErrProbeReceiptInvalid)
	_, err = codec.Encode(Principal{ConnectionID: "connection-1", Generation: "generation-1"}, "https://example.com/mcp", "digest-1", probeReceiptTestTime)
	require.ErrorIs(t, err, ErrProbeReceiptInvalid, "an org-less principal must not mint receipts")
}
