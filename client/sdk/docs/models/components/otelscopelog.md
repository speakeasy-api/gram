# OTELScopeLog

OTEL scope logs container

## Example Usage

```typescript
import { OTELScopeLog } from "@gram/client/models/components/otelscopelog.js";

let value: OTELScopeLog = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `logRecords`                                                           | [components.OTELLogRecord](../../models/components/otellogrecord.md)[] | :heavy_minus_sign:                                                     | Array of log records                                                   |
| `scope`                                                                | [components.OTELScope](../../models/components/otelscope.md)           | :heavy_minus_sign:                                                     | OTEL instrumentation scope                                             |