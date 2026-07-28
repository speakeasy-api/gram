# UpsertOtelForwardingConfigRequest

## Example Usage

```typescript
import { UpsertOtelForwardingConfigRequest } from "@gram/client/models/operations/upsertotelforwardingconfig.js";

let value: UpsertOtelForwardingConfigRequest = {
  upsertConfigRequestBody2: {
    enabled: true,
    endpointUrl: "https://black-and-white-dish.biz/",
  },
};
```

## Fields

| Field                      | Type                                                                                       | Required           | Description    |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                  | _string_                                                                                   | :heavy_minus_sign: | API Key header |
| `gramSession`              | _string_                                                                                   | :heavy_minus_sign: | Session header |
| `upsertConfigRequestBody2` | [components.UpsertConfigRequestBody2](../../models/components/upsertconfigrequestbody2.md) | :heavy_check_mark: | N/A            |
