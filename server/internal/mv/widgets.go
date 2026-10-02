package mv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	widgetsrepo "github.com/speakeasy-api/gram/server/internal/widgets/repo"
)

// BuildWidgetView renders a widget. invalidReason is what validation said is
// wrong with it, if anything. A stored query or visualization that is not
// even a JSON object is reported the same way rather than failing the whole
// list.
func BuildWidgetView(row widgetsrepo.Widget, invalidReason string) *gen.Widget {
	query, reason := decodeWidgetObject("query", row.Query)
	if invalidReason == "" {
		invalidReason = reason
	}
	visualization, reason := decodeWidgetObject("visualization", row.Visualization)
	if invalidReason == "" {
		invalidReason = reason
	}

	return &gen.Widget{
		ID:              row.ID.String(),
		ProjectID:       row.ProjectID.String(),
		OrganizationID:  row.OrganizationID,
		CreatedByUserID: conv.FromPGText[string](row.CreatedByUserID),
		Name:            row.Name,
		Description:     conv.FromPGText[string](row.Description),
		Dataset:         row.Dataset,
		Query:           query,
		Visualization:   visualization,
		InvalidReason:   conv.PtrEmpty(invalidReason),
		CreatedAt:       conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(row.UpdatedAt),
	}
}

// decodeWidgetObject reads a stored JSON object, returning an empty one and a
// reason when it is not one. A stored null decodes into a nil map without
// error, which would render the required field as null, so it is refused too.
func decodeWidgetObject(name string, raw []byte) (map[string]any, string) {
	out := map[string]any{}
	if err := decodeKeepingNumbers(raw, &out); err != nil {
		return map[string]any{}, fmt.Sprintf("%s is not a JSON object: %s", name, err.Error())
	}
	if out == nil {
		return map[string]any{}, name + " is not a JSON object: stored value is null"
	}
	return out, ""
}

// decodeKeepingNumbers reads a stored object keeping its numbers as written.
// Plain json.Unmarshal routes every number through float64, which silently
// rounds anything past that type's exact integer range, and these objects
// are client-owned so they must come back as they went in. The widgets
// service decodes request bodies the same way, so numbers stay exact on the
// way in too. Trailing input is still refused, as Unmarshal would refuse it.
func decodeKeepingNumbers(raw []byte, into *map[string]any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing input")
	}

	return nil
}
