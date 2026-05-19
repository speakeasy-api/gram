package id

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromWorkOSID_Deterministic(t *testing.T) {
	t.Parallel()

	workosOrgID := "org_01JX8J8JQ8Y3Z1X2Y3Z4A5B6C7"
	id1 := FromWorkOSID(workosOrgID)
	id2 := FromWorkOSID(workosOrgID)

	assert.Equal(t, id1, id2, "same input must produce same output")

	parsed, err := uuid.Parse(id1)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(5), parsed.Version())
}

func TestFromWorkOSID_Unique(t *testing.T) {
	t.Parallel()

	id1 := FromWorkOSID("org_01AAA")
	id2 := FromWorkOSID("org_01BBB")

	assert.NotEqual(t, id1, id2, "different inputs must produce different outputs")
}
