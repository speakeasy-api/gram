package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestNetworkIngressRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	original := urn.NewNetworkIngress(id)

	require.Equal(t, "netingress:11111111-1111-1111-1111-111111111111", original.String())

	parsed, err := urn.ParseNetworkIngress(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"netingress:11111111-1111-1111-1111-111111111111"`, string(data))

	var fromJSON urn.NetworkIngress
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.NetworkIngress
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.NetworkIngress
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestNetworkIngressRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseNetworkIngress("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseNetworkIngress("environment:11111111-1111-1111-1111-111111111111")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseNetworkIngress("netingress:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewNetworkIngress(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
