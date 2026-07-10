# OTELScopeMetrics

OTEL scope metrics container

## Example Usage

```typescript
import { OTELScopeMetrics } from "@gram/client/models/components/otelscopemetrics.js";

let value: OTELScopeMetrics = {};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `metrics`                                                        | [components.OTELMetric](../../models/components/otelmetric.md)[] | :heavy_minus_sign:                                               | Array of metrics                                                 |
| `scope`                                                          | [components.OTELScope](../../models/components/otelscope.md)     | :heavy_minus_sign:                                               | OTEL instrumentation scope                                       |