package mv

import (
	"encoding/json"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/conv"
	dashboardsrepo "github.com/speakeasy-api/gram/server/internal/dashboards/repo"
)

// storedDashboardFilters is the shape the dashboards table's filters column
// holds: the date range and the values picked per catalog dimension that a
// dashboard opens on. Keys are omitted when unset, so a fresh dashboard
// stores {}.
type storedDashboardFilters struct {
	Range  *storedDashboardRange `json:"range,omitempty"`
	Values map[string][]string   `json:"values,omitempty"`
}

type storedDashboardRange struct {
	Preset *string `json:"preset,omitempty"`
	From   *string `json:"from,omitempty"`
	To     *string `json:"to,omitempty"`
	Label  *string `json:"label,omitempty"`
}

// EncodeDashboardFilters renders saved filters for storage.
func EncodeDashboardFilters(filters *gen.DashboardFilters) ([]byte, error) {
	stored := storedDashboardFilters{Range: nil, Values: nil}
	if filters != nil {
		if filters.Range != nil {
			stored.Range = &storedDashboardRange{Preset: filters.Range.Preset, From: filters.Range.From, To: filters.Range.To, Label: filters.Range.Label}
		}
		if len(filters.Values) > 0 {
			stored.Values = filters.Values
		}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("encode dashboard filters: %w", err)
	}
	return encoded, nil
}

// BuildDashboardView renders a dashboard with its cards. Stored filters
// that do not decode render as none rather than failing the whole list:
// the dashboard is still there, it just opens on the defaults.
func BuildDashboardView(row dashboardsrepo.Dashboard, placements []dashboardsrepo.DashboardWidget) *gen.Dashboard {
	widgets := make([]*gen.DashboardPlacement, 0, len(placements))
	for _, placement := range placements {
		widgets = append(widgets, &gen.DashboardPlacement{
			ID:       placement.ID.String(),
			WidgetID: placement.WidgetID.String(),
			X:        int(placement.X),
			Y:        int(placement.Y),
			W:        int(placement.W),
			H:        int(placement.H),
		})
	}
	return &gen.Dashboard{
		ID:              row.ID.String(),
		ProjectID:       row.ProjectID.String(),
		OrganizationID:  row.OrganizationID,
		CreatedByUserID: conv.FromPGText[string](row.CreatedByUserID),
		Name:            row.Name,
		Description:     conv.FromPGText[string](row.Description),
		Filters:         decodeDashboardFilters(row.Filters),
		Widgets:         widgets,
		CreatedAt:       conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(row.UpdatedAt),
	}
}

func decodeDashboardFilters(raw []byte) *gen.DashboardFilters {
	out := &gen.DashboardFilters{Range: nil, Values: map[string][]string{}}
	var stored storedDashboardFilters
	if len(raw) == 0 || json.Unmarshal(raw, &stored) != nil {
		return out
	}
	if stored.Range != nil {
		out.Range = &gen.DashboardRange{Preset: stored.Range.Preset, From: stored.Range.From, To: stored.Range.To, Label: stored.Range.Label}
	}
	if stored.Values != nil {
		out.Values = stored.Values
	}
	return out
}
