# PercentileLatencyCutoffs

Percentile-based latency cutoffs. All specified cutoffs must be met for an endpoint to be preferred.


## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `P50`                         | **float64*                    | :heavy_minus_sign:            | Maximum p50 latency (seconds) |
| `P75`                         | **float64*                    | :heavy_minus_sign:            | Maximum p75 latency (seconds) |
| `P90`                         | **float64*                    | :heavy_minus_sign:            | Maximum p90 latency (seconds) |
| `P99`                         | **float64*                    | :heavy_minus_sign:            | Maximum p99 latency (seconds) |