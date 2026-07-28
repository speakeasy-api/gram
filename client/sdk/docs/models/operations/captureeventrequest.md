# CaptureEventRequest

## Example Usage

```typescript
import { CaptureEventRequest } from "@gram/client/models/operations/captureevent.js";

let value: CaptureEventRequest = {
  captureEventPayload: {
    event: "button_clicked",
    properties: {
      button_name: "submit",
      page: "checkout",
      value: 100,
    },
  },
};
```

## Fields

| Field                 | Type                                                                             | Required           | Description                |
| --------------------- | -------------------------------------------------------------------------------- | ------------------ | -------------------------- |
| `gramKey`             | _string_                                                                         | :heavy_minus_sign: | API Key header             |
| `gramSession`         | _string_                                                                         | :heavy_minus_sign: | Session header             |
| `gramProject`         | _string_                                                                         | :heavy_minus_sign: | project header             |
| `gramChatSession`     | _string_                                                                         | :heavy_minus_sign: | Chat Sessions token header |
| `captureEventPayload` | [components.CaptureEventPayload](../../models/components/captureeventpayload.md) | :heavy_check_mark: | N/A                        |
