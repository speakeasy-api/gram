package explore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// dimensionValuesLimit caps how many suggestions the endpoint returns; the
// filter UI is a picker, not a browser.
const dimensionValuesLimit = 50

// dimensionValues returns the dimension's most frequent non-empty canonical
// values in the dataset inside the window.
func dimensionValues(ctx context.Context, conn Conn, projectIDs []uuid.UUID, dataset, dimension string, timeStart, timeEnd int64) ([]string, error) {
	ds, ok := datasetByName(dataset)
	if !ok {
		return nil, &UnknownMemberError{Kind: "dataset", Name: dataset, Detail: ""}
	}
	col, ok := ds.dimensionColumn(dimension)
	if !ok {
		return nil, &UnknownMemberError{Kind: "dimension", Name: dimension, Detail: fmt.Sprintf("dataset %q carries no dimension %q", ds.Name, dimension)}
	}

	canonicalSQL, canonicalArgs, err := compileCanonical(ds, Query{
		Dataset:            ds.Name,
		Calculations:       nil,
		GroupBy:            nil,
		GroupExpressions:   nil,
		Filters:            nil,
		TimeStart:          timeStart,
		TimeEnd:            timeEnd,
		GranularitySeconds: 0,
		ProjectIDs:         projectIDs,
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`WITH canonical AS (%[1]s)
		SELECT %[2]s AS v
		FROM canonical
		WHERE %[3]s >= fromUnixTimestamp64Nano(?)
		  AND %[3]s <= fromUnixTimestamp64Nano(?)
		  AND v != ''
		GROUP BY v
		ORDER BY count() DESC, v ASC
		LIMIT %[4]d`, canonicalSQL, col, canonicalColumn(ds.TimeColumn), dimensionValuesLimit)

	args := make([]any, 0, len(canonicalArgs)+2)
	args = append(args, canonicalArgs...)
	args = append(args, timeStart, timeEnd)
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying %s values on %s: %w", dimension, ds.Name, err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	values := make([]string, 0, dimensionValuesLimit)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning %s values on %s: %w", dimension, ds.Name, err)
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading %s values on %s: %w", dimension, ds.Name, err)
	}
	return values, nil
}
