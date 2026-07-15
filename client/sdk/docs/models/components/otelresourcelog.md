# OTELResourceLog

OTEL resource logs container

## Example Usage

```typescript
import { OTELResourceLog } from "@gram/client/models/components/otelresourcelog.js";

let value: OTELResourceLog = {};
```

## Fields

| Field       | Type                                                                 | Required           | Description               |
| ----------- | -------------------------------------------------------------------- | ------------------ | ------------------------- |
| `resource`  | [components.OTELResource](../../models/components/otelresource.md)   | :heavy_minus_sign: | OTEL resource information |
| `scopeLogs` | [components.OTELScopeLog](../../models/components/otelscopelog.md)[] | :heavy_minus_sign: | Array of scope logs       |
