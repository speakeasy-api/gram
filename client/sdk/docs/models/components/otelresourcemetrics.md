# OTELResourceMetrics

OTEL resource metrics container

## Example Usage

```typescript
import { OTELResourceMetrics } from "@gram/client/models/components/otelresourcemetrics.js";

let value: OTELResourceMetrics = {};
```

## Fields

| Field          | Type                                                                         | Required           | Description               |
| -------------- | ---------------------------------------------------------------------------- | ------------------ | ------------------------- |
| `resource`     | [components.OTELResource](../../models/components/otelresource.md)           | :heavy_minus_sign: | OTEL resource information |
| `scopeMetrics` | [components.OTELScopeMetrics](../../models/components/otelscopemetrics.md)[] | :heavy_minus_sign: | Array of scope metrics    |
