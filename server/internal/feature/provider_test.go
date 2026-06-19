package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestOrgProjectGroups_OrgAndProject(t *testing.T) {
	t.Parallel()

	got := feature.OrgProjectGroups("speakeasy-team", "default")
	require.Equal(t, map[string]string{
		"organization": "speakeasy-team",
		"slug":         "speakeasy-team/default",
	}, got)
}

func TestOrgProjectGroups_OrgOnly(t *testing.T) {
	t.Parallel()

	got := feature.OrgProjectGroups("speakeasy-team", "")
	require.Equal(t, map[string]string{"organization": "speakeasy-team"}, got)
}

func TestOrgProjectGroups_NoOrgReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, feature.OrgProjectGroups("", "default"))
	require.Nil(t, feature.OrgProjectGroups("", ""))
}
