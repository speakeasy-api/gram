# OTELLogRecord

Individual OTEL log record

## Example Usage

```typescript
import { OTELLogRecord } from "@gram/client/models/components/otellogrecord.js";

let value: OTELLogRecord = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `attributes`                                                           | [components.OTELAttribute](../../models/components/otelattribute.md)[] | :heavy_minus_sign:                                                     | Log attributes                                                         |
| `body`                                                                 | [components.OTELLogBody](../../models/components/otellogbody.md)       | :heavy_minus_sign:                                                     | OTEL log body                                                          |
| `droppedAttributesCount`                                               | *number*                                                               | :heavy_minus_sign:                                                     | Number of dropped attributes                                           |
| `observedTimeUnixNano`                                                 | *string*                                                               | :heavy_minus_sign:                                                     | Observed timestamp in nanoseconds                                      |
| `spanId`                                                               | *string*                                                               | :heavy_minus_sign:                                                     | Span ID                                                                |
| `timeUnixNano`                                                         | *string*                                                               | :heavy_minus_sign:                                                     | Timestamp in nanoseconds since Unix epoch                              |
| `traceId`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | Trace ID                                                               |