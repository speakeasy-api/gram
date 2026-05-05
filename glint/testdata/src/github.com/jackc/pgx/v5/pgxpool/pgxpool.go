package pgxpool

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Pool struct{}

func (*Pool) Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error) {
	return pgx.CommandTag{}, nil
}

func (*Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (*Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func (*Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, nil
}

func (*Pool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return nil, nil
}

func (*Pool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (*Pool) CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
