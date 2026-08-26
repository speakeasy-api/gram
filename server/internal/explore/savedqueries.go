package explore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/speakeasy-api/gram/server/internal/explore/repo"
)

// savedQuerySpec is the JSON shape persisted in explore_saved_queries.spec —
// the reusable query definition (the window and chart type are first-class
// columns).
type savedQuerySpec struct {
	Dataset            string            `json:"dataset"`
	Calculations       []Calculation     `json:"calculations"`
	GroupBy            []string          `json:"group_by"`
	GroupExpressions   []GroupExpression `json:"group_expressions"`
	Filters            []Filter          `json:"filters"`
	GranularitySeconds int64             `json:"granularity_seconds"`
	SortBy             string            `json:"sort_by"`
	SortDesc           bool              `json:"sort_desc"`
	Limit              int               `json:"limit"`
}

// savedQuery is one saved query row.
type savedQuery struct {
	ID        string
	Name      string
	ChartType string
	Window    string
	Spec      savedQuerySpec
	CreatedAt time.Time
	UpdatedAt time.Time
}

func encodeSavedQuerySpec(spec savedQuerySpec) ([]byte, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode explore saved query spec: %w", err)
	}
	return data, nil
}

func savedQueryFromRow(row repo.ExploreSavedQuery) (savedQuery, error) {
	var spec savedQuerySpec
	decoder := json.NewDecoder(bytes.NewReader(row.Spec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return savedQuery{}, fmt.Errorf("decode explore saved query spec: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return savedQuery{}, fmt.Errorf("decode explore saved query spec: trailing data")
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return savedQuery{}, fmt.Errorf("explore saved query %s has invalid timestamps", row.ID)
	}

	return savedQuery{
		ID:        row.ID.String(),
		Name:      row.Name,
		ChartType: row.ChartType,
		Window:    row.TimeWindow,
		Spec:      spec,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}
