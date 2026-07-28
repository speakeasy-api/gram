# OTELMetric

OTEL metric

## Example Usage

```typescript
import { OTELMetric } from "@gram/client/models/components/otelmetric.js";

let value: OTELMetric = {};
```

## Fields

| Field                  | Type                                                     | Required           | Description                                       |
| ---------------------- | -------------------------------------------------------- | ------------------ | ------------------------------------------------- |
| `description`          | _string_                                                 | :heavy_minus_sign: | Metric description                                |
| `exponentialHistogram` | _any_                                                    | :heavy_minus_sign: | ExponentialHistogram metric data (passed through) |
| `gauge`                | _any_                                                    | :heavy_minus_sign: | Gauge metric data (passed through)                |
| `histogram`            | _any_                                                    | :heavy_minus_sign: | Histogram metric data (passed through)            |
| `name`                 | _string_                                                 | :heavy_minus_sign: | Metric name                                       |
| `sum`                  | [components.OTELSum](../../models/components/otelsum.md) | :heavy_minus_sign: | OTEL sum metric                                   |
| `summary`              | _any_                                                    | :heavy_minus_sign: | Summary metric data (passed through)              |
| `unit`                 | _string_                                                 | :heavy_minus_sign: | Metric unit                                       |
