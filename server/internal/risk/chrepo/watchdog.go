package chrepo

// WatchdogGroup counts live findings for one whole-window dimension value.
type WatchdogGroup struct {
	Value string
	Count uint64
}

// Positional time.Time parameters are second-precision in clickhouse-go.
// Bind UTC strings through toDateTime64 to preserve bounds and cursor nanos.
const watchdogTimeLayout = "2006-01-02 15:04:05.000000000"

// Resource exhaustion must fail, never return partial results.
const watchdogSettingsSQL = "SETTINGS max_execution_time = 30, max_rows_to_read = 10000000, max_memory_usage = 536870912, read_overflow_mode = 'throw', timeout_overflow_mode = 'throw', group_by_overflow_mode = 'throw', sort_overflow_mode = 'throw', result_overflow_mode = 'throw'"
