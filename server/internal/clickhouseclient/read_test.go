package clickhouseclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestWithReadResilienceRetriesQueryOnFreshConnection(t *testing.T) {
	t.Parallel()

	primary := new(mockConn)
	fresh := new(mockConn)
	rows := new(mockRows)
	firstErr := fmt.Errorf("read first block: %w", io.EOF)
	var firstCtx context.Context
	var retryCtx context.Context

	primary.On("Query", mock.Anything, "SELECT 1", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			firstCtx = ctx
		}).
		Return(nil, firstErr).
		Once()
	fresh.On("Query", mock.Anything, "SELECT 1", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			retryCtx = ctx
		}).
		Return(rows, nil).
		Once()
	rows.On("Next").Return(false).Once()
	rows.On("Close").Return(nil).Once()
	fresh.On("Close").Return(nil).Once()

	factoryCalls := 0
	conn := WithReadResilience(primary, func() (clickhouse.Conn, error) {
		factoryCalls++
		return fresh, nil
	}, time.Minute)

	gotRows, err := conn.Query(t.Context(), "SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, gotRows)
	require.Equal(t, 1, factoryCalls)

	firstDeadline, firstHasDeadline := firstCtx.Deadline()
	retryDeadline, retryHasDeadline := retryCtx.Deadline()
	require.True(t, firstHasDeadline)
	require.True(t, retryHasDeadline)
	require.Equal(t, firstDeadline, retryDeadline)

	require.False(t, gotRows.Next())
	require.ErrorIs(t, firstCtx.Err(), context.Canceled)
	require.NoError(t, gotRows.Close())
	primary.AssertExpectations(t)
	fresh.AssertExpectations(t)
	rows.AssertExpectations(t)
}

func TestWithReadResilienceBoundsQueryAndDoesNotRetryDeadline(t *testing.T) {
	t.Parallel()

	primary := new(mockConn)
	primary.On("Query", mock.Anything, "SELECT sleep", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return(nil, context.DeadlineExceeded).
		Once()

	factoryCalls := 0
	conn := WithReadResilience(primary, func() (clickhouse.Conn, error) {
		factoryCalls++
		return nil, errors.New("must not open a retry connection")
	}, time.Nanosecond)

	rows, err := conn.Query(t.Context(), "SELECT sleep")
	require.Nil(t, rows)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, factoryCalls)
	primary.AssertExpectations(t)
}

func TestWithReadResilienceDoesNotRetryServerError(t *testing.T) {
	t.Parallel()

	primary := new(mockConn)
	serverErr := errors.New("clickhouse exception: invalid query")
	primary.On("Query", mock.Anything, "SELECT invalid", mock.Anything).
		Return(nil, serverErr).
		Once()

	factoryCalls := 0
	conn := WithReadResilience(primary, func() (clickhouse.Conn, error) {
		factoryCalls++
		return nil, errors.New("must not open a retry connection")
	}, time.Minute)

	rows, err := conn.Query(t.Context(), "SELECT invalid")
	require.Nil(t, rows)
	require.ErrorIs(t, err, serverErr)
	require.Zero(t, factoryCalls)
	primary.AssertExpectations(t)
}

func TestWithReadResilienceRetriesQueryRowOnFreshConnection(t *testing.T) {
	t.Parallel()

	primary := new(mockConn)
	fresh := new(mockConn)
	initialRow := new(mockRow)
	retryRow := new(mockRow)
	firstErr := fmt.Errorf("read first block: %w", io.EOF)
	var retryCtx context.Context

	primary.On("QueryRow", mock.Anything, "SELECT count()", mock.Anything).
		Return(initialRow).
		Once()
	initialRow.On("Err").Return(firstErr).Once()
	fresh.On("QueryRow", mock.Anything, "SELECT count()", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			retryCtx = ctx
		}).
		Return(retryRow).
		Once()
	retryRow.On("Scan", mock.Anything).
		Run(func(args mock.Arguments) {
			dest, ok := args.Get(0).([]any)
			require.True(t, ok)
			require.Len(t, dest, 1)
			count, ok := dest[0].(*uint64)
			require.True(t, ok)
			*count = 42
		}).
		Return(nil).
		Once()
	fresh.On("Close").Return(nil).Once()

	conn := WithReadResilience(primary, func() (clickhouse.Conn, error) {
		return fresh, nil
	}, time.Minute)

	var count uint64
	err := conn.QueryRow(t.Context(), "SELECT count()").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, uint64(42), count)
	require.ErrorIs(t, retryCtx.Err(), context.Canceled)
	primary.AssertExpectations(t)
	fresh.AssertExpectations(t)
	initialRow.AssertExpectations(t)
	retryRow.AssertExpectations(t)
}

func TestWithReadResilienceDoesNotRetryWrites(t *testing.T) {
	t.Parallel()

	primary := new(mockConn)
	primary.On("Exec", mock.Anything, "INSERT INTO events VALUES (?)", mock.Anything).
		Return(nil).
		Once()

	factoryCalls := 0
	conn := WithReadResilience(primary, func() (clickhouse.Conn, error) {
		factoryCalls++
		return nil, errors.New("must not open a retry connection")
	}, time.Minute)

	require.NoError(t, conn.Exec(t.Context(), "INSERT INTO events VALUES (?)", 42))
	require.Zero(t, factoryCalls)
	primary.AssertExpectations(t)
}

type mockConn struct {
	mock.Mock
	clickhouse.Conn
}

func (m *mockConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	return m.Called(ctx, dest, query, args).Error(0)
}

func (m *mockConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	call := m.Called(ctx, query, args)
	rows, _ := call.Get(0).(driver.Rows)
	return rows, call.Error(1)
}

func (m *mockConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	row, ok := m.Called(ctx, query, args).Get(0).(driver.Row)
	if !ok {
		panic("mock QueryRow result is not driver.Row")
	}
	return row
}

func (m *mockConn) Exec(ctx context.Context, query string, args ...any) error {
	return m.Called(ctx, query, args).Error(0)
}

func (m *mockConn) Close() error {
	return m.Called().Error(0)
}

type mockRows struct {
	mock.Mock
	driver.Rows
}

func (m *mockRows) Next() bool {
	return m.Called().Bool(0)
}

func (m *mockRows) Close() error {
	return m.Called().Error(0)
}

type mockRow struct {
	mock.Mock
	driver.Row
}

func (m *mockRow) Err() error {
	return m.Called().Error(0)
}

func (m *mockRow) Scan(dest ...any) error {
	return m.Called(dest).Error(0)
}

func (m *mockRow) ScanStruct(dest any) error {
	return m.Called(dest).Error(0)
}
