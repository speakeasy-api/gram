# OTELMetricsPayload

OTEL metrics export payload

## Example Usage

```typescript
import { OTELMetricsPayload } from "@gram/client/models/components/otelmetricspayload.js";

let value: OTELMetricsPayload = {};
```

## Fields

| Field             | Type                                                                               | Required           | Description               |
| ----------------- | ---------------------------------------------------------------------------------- | ------------------ | ------------------------- |
| `resourceMetrics` | [components.OTELResourceMetrics](../../models/components/otelresourcemetrics.md)[] | :heavy_minus_sign: | Array of resource metrics |
