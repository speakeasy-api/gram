# HookIngestEvent

Canonical Speakeasy feature event.


## Fields

| Field                                                                         | Type                                                                          | Required                                                                      | Description                                                                   |
| ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `OccurredAt`                                                                  | [*time.Time](https://pkg.go.dev/time#Time)                                    | :heavy_minus_sign:                                                            | RFC3339 timestamp from the local agent. Defaults to receive time when absent. |
| `Type`                                                                        | [components.Type](../../models/components/type.md)                            | :heavy_check_mark:                                                            | Canonical Speakeasy hook event type.                                               |