# IngestHookEventRequest

## Fields

| Field            | Type                                                                         | Required           | Description                                                                                                |
| ---------------- | ---------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `GramKey`        | `*string`                                                                    | :heavy_minus_sign: | API Key header                                                                                             |
| `GramProject`    | `*string`                                                                    | :heavy_minus_sign: | project header                                                                                             |
| `IdempotencyKey` | `*string`                                                                    | :heavy_minus_sign: | Optional per-invocation token reused across retries so the server stores a redelivered event exactly once. |
| `Body`           | [components.IngestRequestBody](../../models/components/ingestrequestbody.md) | :heavy_check_mark: | N/A                                                                                                        |
