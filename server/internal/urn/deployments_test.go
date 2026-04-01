package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDeploymentRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	original := urn.NewDeployment(id)

	require.Equal(t, "deployment:11111111-1111-1111-1111-111111111111", original.String())

	parsed, err := urn.ParseDeployment(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"deployment:11111111-1111-1111-1111-111111111111"`, string(data))

	var fromJSON urn.Deployment
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Deployment
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Deployment
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestDeploymentRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDeployment("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDeployment("environment:11111111-1111-1111-1111-111111111111")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDeployment("deployment:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewDeployment(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
