# HooksNumberMetricsRequest

## Example Usage

```typescript
import { HooksNumberMetricsRequest } from "@gram/client/models/operations/hooksnumbermetrics.js";

let value: HooksNumberMetricsRequest = {
  otelMetricsPayload: {},
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `gramKey`                                                                      | *string*                                                                       | :heavy_minus_sign:                                                             | API Key header                                                                 |
| `gramProject`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | project header                                                                 |
| `otelMetricsPayload`                                                           | [components.OTELMetricsPayload](../../models/components/otelmetricspayload.md) | :heavy_check_mark:                                                             | N/A                                                                            |