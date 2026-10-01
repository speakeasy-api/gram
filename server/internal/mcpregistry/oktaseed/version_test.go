package oktaseed

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersion_TracksTheVendorTable(t *testing.T) {
	t.Parallel()

	before := Version()
	require.Len(t, before, versionHexLength)
	require.Equal(t, before, versionOf(slices.Clone(Vendors)), "the same table yields the same version")

	changed := slices.Clone(Vendors)
	changed[0].Mapping.OINNames = append([]string{"another-key"}, changed[0].Mapping.OINNames...)
	require.NotEqual(t, before, versionOf(changed), "a new OIN name must produce a new version, or deploys would not apply it")
}
