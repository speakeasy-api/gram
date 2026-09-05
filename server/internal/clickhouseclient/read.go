package clickhouseclient

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type readResilientConn struct {
	clickhouse.Conn
	newConn func() (clickhouse.Conn, error)
	timeout time.Duration
}

// WithReadResilience bounds read operations and retries transport failures that
// happen before a result is exposed. The retry uses a newly opened connection;
// writes and partially consumed result sets are never retried.
func WithReadResilience(
	conn clickhouse.Conn,
	newConn func() (clickhouse.Conn, error),
	timeout time.Duration,
) clickhouse.Conn {
	if conn == nil {
		panic("clickhouse connection is nil")
	}
	if newConn == nil {
		panic("clickhouse connection factory is nil")
	}
	if timeout <= 0 {
		panic("clickhouse read timeout must be positive")
	}

	return &readResilientConn{
		Conn:    conn,
		newConn: newConn,
		timeout: timeout,
	}
}

//nolint:wrapcheck // Preserve ClickHouse errors so callers can classify them.
func (c *readResilientConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Select can mutate dest before returning an error, so it is bounded but not
	// retried. Callers that need retry semantics should use Query or QueryRow.
	return c.Conn.Select(ctx, dest, query, args...)
}

//nolint:wrapcheck // Preserve ClickHouse errors so callers can classify them.
func (c *readResilientConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)

	rows, err := c.Conn.Query(ctx, query, args...)
	if !isRetryableReadError(ctx, err) {
		if err != nil {
			cancel()
			return nil, err
		}
		return &managedRows{
			Rows:   rows,
			cancel: cancel,
			conn:   nil,
			once:   sync.Once{},
			err:    nil,
		}, nil
	}

	fresh, openErr := c.newConn()
	if openErr != nil {
		cancel()
		return nil, errors.Join(err, openErr)
	}

	retryRows, retryErr := fresh.Query(ctx, query, args...)
	if retryErr != nil {
		cancel()
		return nil, errors.Join(err, retryErr, fresh.Close())
	}

	return &managedRows{
		Rows:   retryRows,
		cancel: cancel,
		conn:   fresh,
		once:   sync.Once{},
		err:    nil,
	}, nil
}

func (c *readResilientConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)

	row := c.Conn.QueryRow(ctx, query, args...)
	initialErr := row.Err()
	if !isRetryableReadError(ctx, initialErr) {
		return &managedRow{
			Row:        row,
			cancel:     cancel,
			conn:       nil,
			initialErr: nil,
			once:       sync.Once{},
			err:        nil,
		}
	}

	fresh, openErr := c.newConn()
	if openErr != nil {
		cancel()
		return errorRow{err: errors.Join(initialErr, openErr)}
	}

	retryRow := fresh.QueryRow(ctx, query, args...)
	return &managedRow{
		Row:        retryRow,
		cancel:     cancel,
		conn:       fresh,
		initialErr: initialErr,
		once:       sync.Once{},
		err:        nil,
	}
}

func isRetryableReadError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

type managedRows struct {
	driver.Rows
	cancel context.CancelFunc
	conn   clickhouse.Conn
	once   sync.Once
	err    error
}

func (r *managedRows) Next() bool {
	if r.Rows.Next() {
		return true
	}

	_ = r.cleanup()
	return false
}

func (r *managedRows) Close() error {
	return errors.Join(r.Rows.Close(), r.cleanup())
}

func (r *managedRows) cleanup() error {
	r.once.Do(func() {
		r.cancel()
		if r.conn != nil {
			r.err = r.conn.Close()
		}
	})
	return r.err
}

type managedRow struct {
	driver.Row
	cancel     context.CancelFunc
	conn       clickhouse.Conn
	initialErr error
	once       sync.Once
	err        error
}

func (r *managedRow) Err() error {
	err := r.Row.Err()
	if err == nil {
		return nil
	}

	return errors.Join(r.initialErr, err, r.cleanup())
}

func (r *managedRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	if err != nil {
		return errors.Join(r.initialErr, err, r.cleanup())
	}
	return r.cleanup()
}

func (r *managedRow) ScanStruct(dest any) error {
	err := r.Row.ScanStruct(dest)
	if err != nil {
		return errors.Join(r.initialErr, err, r.cleanup())
	}
	return r.cleanup()
}

func (r *managedRow) cleanup() error {
	r.once.Do(func() {
		r.cancel()
		if r.conn != nil {
			r.err = r.conn.Close()
		}
	})
	return r.err
}

type errorRow struct {
	err error
}

func (r errorRow) Err() error           { return r.err }
func (r errorRow) Scan(...any) error    { return r.err }
func (r errorRow) ScanStruct(any) error { return r.err }
