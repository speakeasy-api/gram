package noqueryskipasyncinsertwait

import "github.com/ClickHouse/clickhouse-go/v2"

const waitKey = "wait_for_async_insert"

func fireAndForget() clickhouse.QueryOption {
	return clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 0, // want `set wait_for_async_insert on the connection, not per query: production connections set 0 and the test connection sets 1 so tests read their own writes`
	})
}

func constantKey() clickhouse.QueryOption {
	return clickhouse.WithSettings(clickhouse.Settings{
		waitKey: 0, // want `set wait_for_async_insert on the connection, not per query`
	})
}

func acknowledged() clickhouse.QueryOption {
	return clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 1,
	})
}

func connectionLevel() *clickhouse.Options {
	return &clickhouse.Options{
		Settings: clickhouse.Settings{
			"wait_for_async_insert": 0,
		},
	}
}
