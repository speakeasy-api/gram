# HooksNumberLogsRequest

## Example Usage

```typescript
import { HooksNumberLogsRequest } from "@gram/client/models/operations/hooksnumberlogs.js";

let value: HooksNumberLogsRequest = {
  otelLogsPayload: {},
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `gramKey`                                                                | *string*                                                                 | :heavy_minus_sign:                                                       | API Key header                                                           |
| `gramProject`                                                            | *string*                                                                 | :heavy_minus_sign:                                                       | project header                                                           |
| `otelLogsPayload`                                                        | [components.OTELLogsPayload](../../models/components/otellogspayload.md) | :heavy_check_mark:                                                       | N/A                                                                      |