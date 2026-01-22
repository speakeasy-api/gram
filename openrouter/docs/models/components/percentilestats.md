# PercentileStats

Latency percentiles in milliseconds over the last 30 minutes. Latency measures time to first token. Only visible when authenticated with an API key or cookie; returns null for unauthenticated requests.


## Fields

| Field                    | Type                     | Required                 | Description              | Example                  |
| ------------------------ | ------------------------ | ------------------------ | ------------------------ | ------------------------ |
| `P50`                    | *float64*                | :heavy_check_mark:       | Median (50th percentile) | 25.5                     |
| `P75`                    | *float64*                | :heavy_check_mark:       | 75th percentile          | 35.2                     |
| `P90`                    | *float64*                | :heavy_check_mark:       | 90th percentile          | 48.7                     |
| `P99`                    | *float64*                | :heavy_check_mark:       | 99th percentile          | 85.3                     |