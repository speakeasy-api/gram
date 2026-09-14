package mv

import (
	"encoding/json"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
	"github.com/speakeasy-api/gram/server/internal/conv"
	explorerepo "github.com/speakeasy-api/gram/server/internal/explore/repo"
)

// BuildExploreQueryView renders a saved query. invalidReason is what the
// catalog said is wrong with the stored spec, if anything; a spec that is
// not even a JSON object is reported the same way rather than failing the
// whole list.
func BuildExploreQueryView(row explorerepo.Query, invalidReason string) *gen.ExploreQuery {
	spec := map[string]any{}
	if err := json.Unmarshal(row.Spec, &spec); err != nil {
		spec = map[string]any{}
		if invalidReason == "" {
			invalidReason = "spec is not a JSON object: " + err.Error()
		}
	}

	return &gen.ExploreQuery{
		ID:              row.ID.String(),
		ProjectID:       row.ProjectID.String(),
		OrganizationID:  row.OrganizationID,
		CreatedByUserID: conv.FromPGText[string](row.CreatedByUserID),
		Name:            row.Name,
		Dataset:         row.Dataset,
		Spec:            spec,
		InvalidReason:   conv.PtrEmpty(invalidReason),
		CreatedAt:       conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(row.UpdatedAt),
	}
}
