package killswitches

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestLifecycleTransactionQueriesRejectTransactionControlSessionStateAndMultipleStatements(t *testing.T) {
	t.Parallel()

	tests := []string{
		"BEGIN",
		" /* leading /* nested */ comment */ COMMIT AND CHAIN",
		"-- leading comment\nEND",
		"ABORT",
		"ROLLBACK TO SAVEPOINT before_change",
		"SAVEPOINT before_change",
		"RELEASE SAVEPOINT before_change",
		"START /* split keyword */ TRANSACTION",
		"PREPARE /* split keyword */ TRANSACTION 'prepared_change'",
		"SET TRANSACTION ISOLATION LEVEL SERIALIZABLE",
		"SET SESSION /* split phrase */ CHARACTERISTICS AS TRANSACTION READ ONLY",
		"SET search_path = attacker",
		"SET ROLE attacker",
		"RESET statement_timeout",
		"DISCARD ALL",
		"SELECT 1; COMMIT",
		"SELECT ';'; ROLLBACK",
		"SELECT $body$; COMMIT$body$; SELECT 2",
		"SELECT 1 AS fooα$tag$; COMMIT -- $tag$",
		"SELECT 1;;",
	}

	for _, sql := range tests {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()
			delegate := &recordingLifecycleTransactionQueries{}
			queries := lifecycleTransactionQueries{db: delegate}

			_, err := queries.Exec(t.Context(), sql)
			require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
			rows, err := queries.Query(t.Context(), sql)
			if rows != nil {
				rows.Close()
			}
			require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
			err = queries.QueryRow(t.Context(), sql).Scan()
			require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
			require.Zero(t, delegate.calls)
		})
	}
}

func TestLifecycleTransactionQueriesRejectQueryRewriters(t *testing.T) {
	t.Parallel()

	delegate := &recordingLifecycleTransactionQueries{}
	queries := lifecycleTransactionQueries{db: delegate}
	rewriter := rewritingLifecycleQuery{sql: "COMMIT"}
	_, err := queries.Exec(t.Context(), "SELECT 1", rewriter)
	require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
	rows, err := queries.Query(t.Context(), "SELECT 1", rewriter)
	if rows != nil {
		rows.Close()
	}
	require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
	err = queries.QueryRow(t.Context(), "SELECT 1", rewriter).Scan()
	require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
	require.Zero(t, delegate.calls)
}

func TestLifecycleTransactionQueriesAllowSingleDomainStatements(t *testing.T) {
	t.Parallel()

	delegate := &recordingLifecycleTransactionQueries{}
	queries := lifecycleTransactionQueries{db: delegate}
	_, err := queries.Exec(t.Context(), "/* domain write */ INSERT INTO audit_events (payload) VALUES ('semi;colon');")
	require.NoError(t, err)
	_, err = queries.Exec(t.Context(), "SET LOCAL statement_timeout = '1s'")
	require.NoError(t, err)
	_, err = queries.Exec(t.Context(), "SET CONSTRAINTS ALL DEFERRED")
	require.NoError(t, err)
	rows, err := queries.Query(t.Context(), `SELECT E'escaped\'quote;still-string' -- trailing comment`)
	require.NoError(t, err)
	if rows != nil {
		rows.Close()
	}
	err = queries.QueryRow(t.Context(), "SELECT $body$BEGIN; COMMIT;$body$; /* trailing */").Scan()
	require.NoError(t, err)
	require.Equal(t, 5, delegate.calls)
}

type rewritingLifecycleQuery struct {
	sql string
}

func (q rewritingLifecycleQuery) RewriteQuery(_ context.Context, _ *pgx.Conn, _ string, _ []any) (string, []any, error) {
	return q.sql, nil, nil
}

type recordingLifecycleTransactionQueries struct {
	calls int
}

func (q *recordingLifecycleTransactionQueries) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	q.calls++
	return pgconn.CommandTag{}, nil
}

func (q *recordingLifecycleTransactionQueries) Query(context.Context, string, ...any) (pgx.Rows, error) {
	q.calls++
	return nil, nil
}

func (q *recordingLifecycleTransactionQueries) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	return successfulLifecycleQueryRow{}
}

type successfulLifecycleQueryRow struct{}

func (successfulLifecycleQueryRow) Scan(...any) error {
	return nil
}
