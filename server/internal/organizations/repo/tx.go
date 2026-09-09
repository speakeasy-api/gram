package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Begin starts a transaction on a Queries value backed by a database pool.
func (q *Queries) Begin(ctx context.Context) (pgx.Tx, error) {
	beginner, ok := q.db.(transactionBeginner)
	if !ok {
		return nil, fmt.Errorf("organization repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin organization transaction: %w", err)
	}
	return tx, nil
}
