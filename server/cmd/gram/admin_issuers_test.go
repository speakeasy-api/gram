package gram

import (
	"encoding/base64"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdminIssuerEncryption(t *testing.T) {
	t.Parallel()
	client, err := newAdminIssuerEncryption("")
	require.NoError(t, err)
	require.Nil(t, client)
	client, err = newAdminIssuerEncryption("invalid")
	require.ErrorContains(t, err, "create remote session encryption client")
	require.Nil(t, client)
	client, err = newAdminIssuerEncryption(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	require.NotNil(t, client)
}
