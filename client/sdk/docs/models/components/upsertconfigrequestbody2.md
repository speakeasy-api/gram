# UpsertConfigRequestBody2

## Example Usage

```typescript
import { UpsertConfigRequestBody2 } from "@gram/client/models/components/upsertconfigrequestbody2.js";

let value: UpsertConfigRequestBody2 = {
  enabled: true,
  endpointUrl: "https://willing-blight.org",
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `enabled`                                                                                      | *boolean*                                                                                      | :heavy_check_mark:                                                                             | Whether forwarding should be active.                                                           |
| `endpointUrl`                                                                                  | *string*                                                                                       | :heavy_check_mark:                                                                             | URL to forward OTEL payloads to.                                                               |
| `headers`                                                                                      | [components.OtelForwardingHeaderInput](../../models/components/otelforwardingheaderinput.md)[] | :heavy_minus_sign:                                                                             | Full set of headers to attach. Replaces any existing headers.                                  |