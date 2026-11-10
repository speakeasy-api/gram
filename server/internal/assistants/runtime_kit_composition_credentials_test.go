package assistants

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestKitLifecycleCredentialReadErrors(t *testing.T) {
	t.Parallel()
	databaseErr := errors.New("database temporarily unavailable")
	for _, tc := range []struct {
		name       string
		readErrors []error
		want       error
	}{
		{name: "absent authority", readErrors: []error{pgx.ErrNoRows}, want: ErrKitRuntimeBinding},
		{name: "wrapped absent authority", readErrors: []error{fmt.Errorf("authority: %w", pgx.ErrNoRows)}, want: ErrKitRuntimeBinding},
		{name: "authority database failure", readErrors: []error{databaseErr}, want: databaseErr},
		{name: "authority transaction failure", readErrors: []error{pgx.ErrTxClosed}, want: pgx.ErrTxClosed},
		{name: "authority cancellation", readErrors: []error{context.Canceled}, want: context.Canceled},
		{name: "absent credential", readErrors: []error{nil, pgx.ErrNoRows}, want: ErrKitGatewayTokenNotFound},
		{name: "credential database failure", readErrors: []error{nil, databaseErr}, want: databaseErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &kitLifecycleCredentialReadDB{t: t, readErrors: tc.readErrors}
			ctx := context.WithValue(t.Context(), runtimeSwitchQueriesKey{}, assistantrepo.New(db))
			// No fallback database or encryption client: failed reads must neither
			// create credentials nor continue through the bootstrap path.
			core := &ServiceCore{}
			token, err := core.loadKitGatewayTokenAlreadyLocked(ctx, assistantRuntimeRecord{ID: uuid.New(), ProjectID: uuid.New(), AssistantID: uuid.New()})
			require.Empty(t, token)
			require.ErrorIs(t, err, tc.want)
			if !errors.Is(tc.want, ErrKitRuntimeBinding) {
				require.NotErrorIs(t, err, ErrKitRuntimeBinding)
			}
			if !errors.Is(tc.want, ErrKitGatewayTokenNotFound) {
				require.NotErrorIs(t, err, ErrKitGatewayTokenNotFound)
			}
			require.Equal(t, len(tc.readErrors), db.reads)
		})
	}
}

// Unimplemented DB operations panic, so the read path cannot silently write.
type kitLifecycleCredentialReadDB struct {
	assistantrepo.DBTX
	t          *testing.T
	readErrors []error
	reads      int
}

func (db *kitLifecycleCredentialReadDB) QueryRow(context.Context, string, ...any) pgx.Row {
	db.t.Helper()
	require.Less(db.t, db.reads, len(db.readErrors), "unexpected credential read after authority failure")
	err := db.readErrors[db.reads]
	db.reads++
	return kitLifecycleCredentialReadRow{err: err}
}

type kitLifecycleCredentialReadRow struct{ err error }

func (row kitLifecycleCredentialReadRow) Scan(...any) error { return row.err }
