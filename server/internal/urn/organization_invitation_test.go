package urn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestOrganizationInvitation_String(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	require.Equal(t, "organization-invitation:55555555-5555-5555-5555-555555555555", urn.NewOrganizationInvitation(id).String())
}

func TestParseOrganizationInvitation(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewOrganizationInvitation(id)

	parsed, err := urn.ParseOrganizationInvitation(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)
}

func TestParseOrganizationInvitation_Invalid(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseOrganizationInvitation("")
	require.Error(t, err)

	_, err = urn.ParseOrganizationInvitation("organization:55555555-5555-5555-5555-555555555555")
	require.Error(t, err)

	_, err = urn.ParseOrganizationInvitation("organization-invitation:not-a-uuid")
	require.Error(t, err)
}
