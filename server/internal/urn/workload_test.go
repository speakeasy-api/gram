package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestWorkloadIssuerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	original := urn.NewWorkloadIssuer(id)

	require.Equal(t, "workload_issuer:66666666-6666-6666-6666-666666666666", original.String())

	parsed, err := urn.ParseWorkloadIssuer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"workload_issuer:66666666-6666-6666-6666-666666666666"`, string(data))

	var fromJSON urn.WorkloadIssuer
	require.NoError(t, json.Unmarshal(data, &fromJSON))
	require.Equal(t, original.ID, fromJSON.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.WorkloadIssuer
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original.ID, fromDB.ID)
}

func TestWorkloadAdmissionRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	original := urn.NewWorkloadAdmission(id)

	require.Equal(t, "workload_admission:77777777-7777-7777-7777-777777777777", original.String())

	parsed, err := urn.ParseWorkloadAdmission(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.WorkloadAdmission
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original.ID, fromText.ID)
}

func TestWorkloadUrnsRejectInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseWorkloadIssuer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// The two are one word apart and both name something in the workload
	// federation path; neither may parse as the other.
	_, err = urn.ParseWorkloadIssuer("workload_admission:66666666-6666-6666-6666-666666666666")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseWorkloadAdmission("workload_issuer:66666666-6666-6666-6666-666666666666")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// And neither may be confused with the machine principal, which is what
	// "workload" names everywhere else in this package.
	_, err = urn.ParseWorkloadIssuer("workload:66666666-6666-6666-6666-666666666666")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseWorkloadAdmission("workload_admission:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewWorkloadIssuer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewWorkloadAdmission(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
