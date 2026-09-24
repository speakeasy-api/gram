package jsonwebkeysets

import (
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// mapKeyUniqueViolation guards races the set lock makes unreachable through
// the current handlers, so no service-level test can drive these branches;
// they are pinned here directly instead.

func uniqueViolation(constraint string) error {
	return &pgconn.PgError{
		Code:           pgerrcode.UniqueViolation,
		ConstraintName: constraint,
	}
}

func TestMapKeyUniqueViolation_OneActiveIndex(t *testing.T) {
	t.Parallel()

	mapped := mapKeyUniqueViolation(uniqueViolation("json_web_keys_one_active_idx"))
	require.Error(t, mapped)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, mapped, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestMapKeyUniqueViolation_SetKidIndex(t *testing.T) {
	t.Parallel()

	mapped := mapKeyUniqueViolation(uniqueViolation("json_web_keys_set_kid_idx"))
	require.Error(t, mapped)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, mapped, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

// The two indexes carry different remediation, so their conflicts must not
// collapse into one message.
func TestMapKeyUniqueViolation_DistinctMessages(t *testing.T) {
	t.Parallel()

	oneActive := mapKeyUniqueViolation(uniqueViolation("json_web_keys_one_active_idx"))
	setKid := mapKeyUniqueViolation(uniqueViolation("json_web_keys_set_kid_idx"))
	require.NotEqual(t, oneActive.Error(), setKid.Error())
}

func TestMapKeyUniqueViolation_UnknownConstraintPassesThrough(t *testing.T) {
	t.Parallel()

	require.NoError(t, mapKeyUniqueViolation(uniqueViolation("some_other_unique_idx")))
}

func TestMapKeyUniqueViolation_NonUniqueViolationPassesThrough(t *testing.T) {
	t.Parallel()

	require.NoError(t, mapKeyUniqueViolation(&pgconn.PgError{
		Code:           pgerrcode.ForeignKeyViolation,
		ConstraintName: "json_web_keys_one_active_idx",
	}))
	require.NoError(t, mapKeyUniqueViolation(errors.New("not a pg error")))
}
