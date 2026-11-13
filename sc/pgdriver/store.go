package pgdriver

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/sc"
)

// Querier runs queries: a pool, a connection, or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is where a [Driver] keeps each instance's snapshot. Every method
// runs inside the transaction the driver was given, so a store can keep the
// snapshot on the domain's own row, copy the state into the columns that
// queries and indexes read, and write an audit row in the same transaction
// as the state change.
//
// The default store, [TableStore], keeps snapshots in the driver's own table.
type Store[S comparable, C any] interface {
	// Lock reads an instance's stored snapshot and locks it until the
	// transaction ends. It returns an error wrapping [ErrNotFound] when the
	// instance does not exist.
	Lock(ctx context.Context, tx pgx.Tx, id string) ([]byte, error)

	// Read reads an instance's stored snapshot without locking it. It
	// returns an error wrapping [ErrNotFound] when the instance does not
	// exist.
	Read(ctx context.Context, q Querier, id string) ([]byte, error)

	// Insert stores a new instance's first snapshot, encoded as data. It
	// returns an error wrapping [ErrExists] when the ID is taken.
	Insert(ctx context.Context, tx pgx.Tx, id string, snap sc.Snapshot[S, C], data []byte) error

	// Update replaces an instance's snapshot after step, with the new
	// snapshot encoded as data. It is called only for accepted steps.
	Update(ctx context.Context, tx pgx.Tx, id string, step sc.Step[S, C], data []byte) error
}

// TableStore keeps snapshots in the statechart_instances table that
// [Schema] creates, one row per instance of the machine.
type TableStore[S comparable, C any] struct {
	// Machine is the ID of the machine whose instances the store holds.
	Machine string
}

// Lock reads and locks the instance's row.
func (s TableStore[S, C]) Lock(ctx context.Context, tx pgx.Tx, id string) ([]byte, error) {
	return s.read(ctx, tx, id, `SELECT snapshot FROM statechart_instances WHERE machine = $1 AND id = $2 FOR UPDATE`)
}

// Read reads the instance's row.
func (s TableStore[S, C]) Read(ctx context.Context, q Querier, id string) ([]byte, error) {
	return s.read(ctx, q, id, `SELECT snapshot FROM statechart_instances WHERE machine = $1 AND id = $2`)
}

func (s TableStore[S, C]) read(ctx context.Context, q Querier, id, query string) ([]byte, error) {
	var data []byte

	err := q.QueryRow(ctx, query, s.Machine, id).Scan(&data)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("read instance: %w", err)
	}

	return data, nil
}

// Insert adds the instance's row.
func (s TableStore[S, C]) Insert(ctx context.Context, tx pgx.Tx, id string, snap sc.Snapshot[S, C], data []byte) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO statechart_instances (machine, id, seq, snapshot)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (machine, id) DO NOTHING`,
		s.Machine, id, snap.Seq(), data,
	)
	if err != nil {
		return fmt.Errorf("insert instance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExists
	}

	return nil
}

// Update rewrites the instance's row.
func (s TableStore[S, C]) Update(ctx context.Context, tx pgx.Tx, id string, step sc.Step[S, C], data []byte) error {
	_, err := tx.Exec(ctx, `
		UPDATE statechart_instances
		SET seq = $3, snapshot = $4, updated_at = clock_timestamp()
		WHERE machine = $1 AND id = $2`,
		s.Machine, id, step.To.Seq(), data,
	)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}

	return nil
}
