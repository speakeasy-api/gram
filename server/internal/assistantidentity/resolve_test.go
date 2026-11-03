package assistantidentity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type stableResolutionTx struct {
	pgx.Tx
	rows []pgx.Row
}

func (tx *stableResolutionTx) QueryRow(context.Context, string, ...any) pgx.Row {
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return row
}

type stableResolutionRow func(...any) error

func (row stableResolutionRow) Scan(dest ...any) error { return row(dest...) }

func TestResolveStableMissingState(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		binding bool
		err     error
		state   State
	}{
		{"missing binding", false, pgx.ErrNoRows, NeverConfigured},
		{"deleted agent", true, pgx.ErrNoRows, Unavailable},
		{"binding query failure", false, dbErr, ""},
		{"agent query failure", true, dbErr, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &stableResolutionTx{}
			if tc.binding {
				tx.rows = append(tx.rows, stableResolutionRow(func(dest ...any) error {
					*dest[1].(*string) = "org-test"
					*dest[2].(*uuid.UUID) = uuid.New()
					return nil
				}))
			}
			tx.rows = append(tx.rows, stableResolutionRow(func(...any) error { return tc.err }))
			result, err := resolve(t.Context(), tx, "org-test", uuid.New(), uuid.New(), uuid.Nil)
			if errors.Is(tc.err, dbErr) {
				require.ErrorIs(t, err, dbErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, result.State)
				require.Nil(t, result.Identity)
			}
			require.Empty(t, tx.rows)
		})
	}
}
