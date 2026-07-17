package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
)

// BuildModelProviderKeyView builds the API response type for a key row. Key
// material is deliberately absent from the response type.
func BuildModelProviderKeyView(key repo.ModelProviderKey) *types.ModelProviderKey {
	return &types.ModelProviderKey{
		ID:        key.ID.String(),
		ProjectID: key.ProjectID.String(),
		Slot:      key.Slot,
		Provider:  key.Provider,
		Enabled:   key.Enabled,
		CreatedAt: conv.FromPGTimestamptz(key.CreatedAt),
		UpdatedAt: conv.FromPGTimestamptz(key.UpdatedAt),
	}
}

func BuildModelProviderKeyListView(keys []repo.ModelProviderKey) []*types.ModelProviderKey {
	result := make([]*types.ModelProviderKey, len(keys))
	for i, key := range keys {
		result[i] = BuildModelProviderKeyView(key)
	}

	return result
}
