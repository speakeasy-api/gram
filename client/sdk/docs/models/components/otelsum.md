# OTELSum

OTEL sum metric

## Example Usage

```typescript
import { OTELSum } from "@gram/client/models/components/otelsum.js";

let value: OTELSum = {};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `aggregationTemporality`                                                           | *any*                                                                              | :heavy_minus_sign:                                                                 | Aggregation temporality (number or enum string)                                    |
| `dataPoints`                                                                       | [components.OTELNumberDataPoint](../../models/components/otelnumberdatapoint.md)[] | :heavy_minus_sign:                                                                 | Data points                                                                        |
| `isMonotonic`                                                                      | *boolean*                                                                          | :heavy_minus_sign:                                                                 | Whether the sum is monotonic                                                       |