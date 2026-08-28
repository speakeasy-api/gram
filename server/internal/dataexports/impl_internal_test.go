package dataexports

import (
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRouteSourceConflictMapsProjectSourceIndexViolation(t *testing.T) {
	t.Parallel()

	databaseError := &pgconn.PgError{
		Code:           pgerrcode.UniqueViolation,
		ConstraintName: "data_export_routes_project_source_key",
	}

	err := routeSourceConflict(databaseError)
	require.NotNil(t, err)
	require.Equal(t, oops.CodeConflict, err.Code)
	require.Equal(t, "a route already exists for this data source", err.Error())
}

func TestRouteSourceConflictIgnoresOtherUniqueViolations(t *testing.T) {
	t.Parallel()

	databaseError := &pgconn.PgError{
		Code:           pgerrcode.UniqueViolation,
		ConstraintName: "another_unique_index",
	}

	require.Nil(t, routeSourceConflict(databaseError))
}
