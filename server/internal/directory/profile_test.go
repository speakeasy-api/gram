package directory_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/directory"
)

func TestUserProfileAttributes(t *testing.T) {
	t.Parallel()

	profile := directory.UserProfile{
		DepartmentName: "Engineering",
		DivisionName:   "Platform",
		CostCenterName: "Research",
		RawAttributes:  map[string]any{"custom_thing": "not projected"},
	}

	require.Equal(t, directory.UserAttributes{
		DepartmentName: "Engineering",
		DivisionName:   "Platform",
		CostCenterName: "Research",
	}, profile.Attributes())
}

func TestUserProfileGroupNames(t *testing.T) {
	t.Parallel()

	profile := directory.UserProfile{Groups: []directory.Group{
		{ExternalID: "group_engineering", Name: "Engineering"},
		{ExternalID: "group_platform", Name: "Platform"},
	}}

	require.Equal(t, []string{"Engineering", "Platform"}, profile.GroupNames())
	require.NotNil(t, (directory.UserProfile{}).GroupNames())
}
