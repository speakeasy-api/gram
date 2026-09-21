package mv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
	if err := decodeSpec(row.Spec, &spec); err != nil {
		spec = map[string]any{}
		if invalidReason == "" {
			invalidReason = "spec is not a JSON object: " + err.Error()
		}
	} else if spec == nil {
		// A stored null decodes into a nil map without error, which would
		// render the required spec as null.
		spec = map[string]any{}
		if invalidReason == "" {
			invalidReason = "spec is not a JSON object: stored value is null"
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

// decodeSpec reads a stored spec keeping its numbers as written. Plain
// json.Unmarshal routes every number through float64, which silently rounds
// anything past that type's exact integer range, and the spec is client-owned
// so it must come back as it went in. Trailing input is still refused, as
// Unmarshal would refuse it.
func decodeSpec(raw []byte, into *map[string]any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode spec: %w", err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing input after spec")
	}

	return nil
}
