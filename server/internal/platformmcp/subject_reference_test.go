package platformmcp

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testReferencePrincipal() Principal {
	return Principal{
		OrganizationID: "org-1",
		ConnectionID:   "connection-1",
		Generation:     "generation-1",
		UserID:         "user-1",
	}
}

func TestSubjectReference_RoundTripsWithinItsSession(t *testing.T) {
	t.Parallel()

	codec, err := newSubjectReferenceCodec("key-material")
	require.NoError(t, err)

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	principal := testReferencePrincipal()

	reference, err := codec.Encode(principal, subjectKindUser, FormatSubjectIdentity(SubjectIdentityEmail, "alice@example.com"), now)
	require.NoError(t, err)

	// Decoded before asserting, because base64 is an encoding and not
	// confidentiality: a substring check against the token text would pass for
	// a plaintext payload and prove nothing.
	raw, err := base64.RawURLEncoding.DecodeString(reference)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "alice")
	require.NotContains(t, string(raw), "example.com")
	require.NotContains(t, string(raw), "org-1")
	require.NotContains(t, string(raw), subjectKindUser)

	value, err := codec.Decode(reference, principal, subjectKindUser, now)
	require.NoError(t, err)
	require.Equal(t, FormatSubjectIdentity(SubjectIdentityEmail, "alice@example.com"), value)
}

func TestSubjectReference_Rejected(t *testing.T) {
	t.Parallel()

	codec, err := newSubjectReferenceCodec("key-material")
	require.NoError(t, err)

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	principal := testReferencePrincipal()
	reference, err := codec.Encode(principal, subjectKindUser, FormatSubjectIdentity(SubjectIdentityEmail, "alice@example.com"), now)
	require.NoError(t, err)

	otherOrg := testReferencePrincipal()
	otherOrg.OrganizationID = "org-2"

	reauthorized := testReferencePrincipal()
	reauthorized.Generation = "generation-2"

	tests := []struct {
		name      string
		token     string
		principal Principal
		kind      string
		at        time.Time
	}{
		{
			name:      "another organization",
			token:     reference,
			principal: otherOrg,
			kind:      subjectKindUser,
			at:        now,
		},
		{
			// Reauthorization ends the session the reference was minted for,
			// exactly as it ends a pagination cursor's.
			name:      "after reauthorization",
			token:     reference,
			principal: reauthorized,
			kind:      subjectKindUser,
			at:        now,
		},
		{
			// A trace handle must never be spendable where a person is
			// expected, or a caller could launder one kind into the other.
			name:      "wrong kind",
			token:     reference,
			principal: principal,
			kind:      subjectKindTrace,
			at:        now,
		},
		{
			name:      "expired",
			token:     reference,
			principal: principal,
			kind:      subjectKindUser,
			at:        now.Add(SubjectReferenceTTL + time.Second),
		},
		{
			// Rejected exactly at the boundary, so the advertised lifetime is a
			// limit rather than a floor.
			name:      "at the expiry instant",
			token:     reference,
			principal: principal,
			kind:      subjectKindUser,
			at:        now.Add(SubjectReferenceTTL),
		},
		{
			name:      "not a reference",
			token:     "definitely-not-a-reference",
			principal: principal,
			kind:      subjectKindUser,
			at:        now,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := codec.Decode(test.token, test.principal, test.kind, test.at)
			require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
		})
	}
}

// TestSubjectReference_ForgedSignatureRejected pins that the payload is never
// trusted before its signature is. A caller that guesses the JSON shape must
// not be able to mint a reference to a subject it was never shown.
func TestSubjectReference_ForgedSignatureRejected(t *testing.T) {
	t.Parallel()

	codec, err := newSubjectReferenceCodec("key-material")
	require.NoError(t, err)
	other, err := newSubjectReferenceCodec("different-key-material")
	require.NoError(t, err)

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	principal := testReferencePrincipal()

	forged, err := other.Encode(principal, subjectKindUser, FormatSubjectIdentity(SubjectIdentityEmail, "alice@example.com"), now)
	require.NoError(t, err)

	_, err = codec.Decode(forged, principal, subjectKindUser, now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

// TestSubjectReference_UnavailableWithoutKeyMaterial pins that a deployment
// with no key material cannot mint references at all, rather than minting
// unbound ones.
func TestSubjectReference_UnavailableWithoutKeyMaterial(t *testing.T) {
	t.Parallel()

	_, err := newSubjectReferenceCodec("")
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}
