package urn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestBillingMetadata_String(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	require.Equal(t, "billing-metadata:55555555-5555-5555-5555-555555555555", urn.NewBillingMetadata(id).String())
}

func TestParseBillingMetadata(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewBillingMetadata(id)

	parsed, err := urn.ParseBillingMetadata(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)
}

func TestParseBillingMetadata_Invalid(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseBillingMetadata("")
	require.Error(t, err)

	_, err = urn.ParseBillingMetadata("billing:55555555-5555-5555-5555-555555555555")
	require.Error(t, err)

	_, err = urn.ParseBillingMetadata("billing-metadata:not-a-uuid")
	require.Error(t, err)
}
