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

func otherZeroSpellings() []clickhouse.Settings {
	return []clickhouse.Settings{
		{"wait_for_async_insert": 0.0},     // want `set wait_for_async_insert on the connection, not per query`
		{"wait_for_async_insert": false},   // want `set wait_for_async_insert on the connection, not per query`
		{"wait_for_async_insert": "0"},     // want `set wait_for_async_insert on the connection, not per query`
		{"wait_for_async_insert": "false"}, // want `set wait_for_async_insert on the connection, not per query`
	}
}

func builtThenPassed(lift bool) clickhouse.QueryOption {
	settings := clickhouse.Settings{"insert_deduplication_token": "token"}
	if lift {
		settings["wait_for_async_insert"] = 0 // want `set wait_for_async_insert on the connection, not per query`
	}
	return clickhouse.WithSettings(settings)
}

func acknowledged() clickhouse.QueryOption {
	settings := clickhouse.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 1,
	}
	settings["wait_for_async_insert"] = true
	settings["wait_for_async_insert"] = "1"
	return clickhouse.WithSettings(settings)
}

func connectionLiteral() *clickhouse.Options {
	return &clickhouse.Options{
		Settings: clickhouse.Settings{
			"wait_for_async_insert": 0,
		},
	}
}

func connectionAssignments() *clickhouse.Options {
	opts := &clickhouse.Options{}
	opts.Settings = clickhouse.Settings{"wait_for_async_insert": 0}
	opts.Settings["wait_for_async_insert"] = 0
	return opts
}
