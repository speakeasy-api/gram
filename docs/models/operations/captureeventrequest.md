# CaptureEventRequest

## Example Usage

```typescript
import { CaptureEventRequest } from "@gram/client/models/operations";

let value: CaptureEventRequest = {
  captureEventPayload: {
    event: "button_clicked",
    properties: {
      "button_name": "submit",
      "page": "checkout",
      "value": 100,
    },
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramKey`                                                                        | *string*                                                                         | :heavy_minus_sign:                                                               | API Key header                                                                   |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `gramProject`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | project header                                                                   |
| `gramChatSession`                                                                | *string*                                                                         | :heavy_minus_sign:                                                               | Chat Sessions token header                                                       |
| `captureEventPayload`                                                            | [components.CaptureEventPayload](../../models/components/captureeventpayload.md) | :heavy_check_mark:                                                               | N/A                                                                              |