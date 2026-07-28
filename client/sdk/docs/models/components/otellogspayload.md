# OTELLogsPayload

OTEL logs export payload

## Example Usage

```typescript
import { OTELLogsPayload } from "@gram/client/models/components/otellogspayload.js";

let value: OTELLogsPayload = {};
```

## Fields

| Field          | Type                                                                       | Required           | Description            |
| -------------- | -------------------------------------------------------------------------- | ------------------ | ---------------------- |
| `resourceLogs` | [components.OTELResourceLog](../../models/components/otelresourcelog.md)[] | :heavy_minus_sign: | Array of resource logs |
