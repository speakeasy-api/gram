package pgx

import (
	"context"
	"errors"
)

var ErrNoRows = errors.New("no rows in result set")

type CommandTag struct{}

type Rows interface{}

type Row interface {
	Scan(dest ...any) error
}

type Identifier []string

type CopyFromSource interface{}

type BatchResults interface{}

type Batch struct{}

type TxOptions struct{}

type Conn struct{}

func (*Conn) Exec(ctx context.Context, sql string, args ...any) (CommandTag, error) {
	return CommandTag{}, nil
}

func (*Conn) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return nil, nil
}

func (*Conn) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return nil
}

func (*Conn) Begin(ctx context.Context) (Tx, error) {
	return nil, nil
}

func (*Conn) BeginTx(ctx context.Context, opts TxOptions) (Tx, error) {
	return nil, nil
}

func (*Conn) SendBatch(ctx context.Context, b *Batch) BatchResults {
	return nil
}

func (*Conn) CopyFrom(ctx context.Context, table Identifier, cols []string, src CopyFromSource) (int64, error) {
	return 0, nil
}

type Tx interface {
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Begin(ctx context.Context) (Tx, error)
	SendBatch(ctx context.Context, b *Batch) BatchResults
	CopyFrom(ctx context.Context, table Identifier, cols []string, src CopyFromSource) (int64, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}
