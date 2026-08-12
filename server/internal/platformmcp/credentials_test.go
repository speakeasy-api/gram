package platformmcp

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

func TestCredentialCodec_RoundTripKeepsOrganizationOpaque(t *testing.T) {
	t.Parallel()

	codec := newTestCredentialCodec(t)
	credential, err := codec.Issue(refreshTokenCredential, "organization-1")
	require.NoError(t, err)
	require.NotContains(t, credential, "organization-1")

	organizationID, err := codec.OrganizationID(refreshTokenCredential, credential)
	require.NoError(t, err)
	require.Equal(t, "organization-1", organizationID)
}

func TestCredentialCodec_RejectsWrongKindAndTampering(t *testing.T) {
	t.Parallel()

	codec := newTestCredentialCodec(t)
	credential, err := codec.Issue(authorizationCodeCredential, "organization-1")
	require.NoError(t, err)

	_, err = codec.OrganizationID(refreshTokenCredential, credential)
	require.Error(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(credential)
	require.NoError(t, err)
	raw[len(raw)/2] ^= 1
	_, err = codec.OrganizationID(authorizationCodeCredential, base64.RawURLEncoding.EncodeToString(raw))
	require.Error(t, err)
}

func TestCredentialCodec_RejectsOversizedCredential(t *testing.T) {
	t.Parallel()

	codec := newTestCredentialCodec(t)
	_, err := codec.OrganizationID(accessJTICredential, strings.Repeat("a", maxPlatformCredentialLength+1))
	require.Error(t, err)
}

func newTestCredentialCodec(t *testing.T) *CredentialCodec {
	t.Helper()

	encryptionClient, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	codec, err := NewCredentialCodec(encryptionClient)
	require.NoError(t, err)
	return codec
}
