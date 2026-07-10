# OTELNumberDataPoint

OTEL number data point

## Example Usage

```typescript
import { OTELNumberDataPoint } from "@gram/client/models/components/otelnumberdatapoint.js";

let value: OTELNumberDataPoint = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `asDouble`                                                             | *number*                                                               | :heavy_minus_sign:                                                     | Value as double                                                        |
| `asInt`                                                                | *any*                                                                  | :heavy_minus_sign:                                                     | Value as integer (string-encoded per OTLP/JSON, or raw number)         |
| `attributes`                                                           | [components.OTELAttribute](../../models/components/otelattribute.md)[] | :heavy_minus_sign:                                                     | Data point attributes                                                  |
| `startTimeUnixNano`                                                    | *string*                                                               | :heavy_minus_sign:                                                     | Start timestamp in nanoseconds                                         |
| `timeUnixNano`                                                         | *string*                                                               | :heavy_minus_sign:                                                     | Timestamp in nanoseconds                                               |