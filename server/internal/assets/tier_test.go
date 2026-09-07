package assets

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAssetStorageKey_Project(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	key, err := assetStorageKey(tierProject, projectID, "org_123", "image-abc.png")
	require.NoError(t, err)
	require.Equal(t, projectID.String()+"/image-abc.png", key)
}

func TestAssetStorageKey_ProjectRejectsEmptyProjectID(t *testing.T) {
	t.Parallel()

	_, err := assetStorageKey(tierProject, uuid.Nil, "org_123", "image-abc.png")
	require.Error(t, err)
}

func TestAssetStorageKey_Organization(t *testing.T) {
	t.Parallel()

	key, err := assetStorageKey(tierOrganization, uuid.Nil, "org_123", "image-abc.png")
	require.NoError(t, err)
	require.Equal(t, "organizations/org_123/image-abc.png", key)
}

func TestAssetStorageKey_OrganizationRejectsEmptyOrganizationID(t *testing.T) {
	t.Parallel()

	_, err := assetStorageKey(tierOrganization, uuid.Nil, "", "image-abc.png")
	require.Error(t, err)
}

func TestAssetStorageKey_Platform(t *testing.T) {
	t.Parallel()

	key, err := assetStorageKey(tierPlatform, uuid.Nil, "", "image-abc.png")
	require.NoError(t, err)
	require.Equal(t, "platform/image-abc.png", key)
}

func TestAssetStorageKey_UnknownTierErrors(t *testing.T) {
	t.Parallel()

	_, err := assetStorageKey(assetTier(""), uuid.Nil, "", "image-abc.png")
	require.Error(t, err)
}
