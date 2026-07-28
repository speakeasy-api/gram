# CaptureEventPayload

Payload for capturing a telemetry event

## Example Usage

```typescript
import { CaptureEventPayload } from "@gram/client/models/components/captureeventpayload.js";

let value: CaptureEventPayload = {
  event: "button_clicked",
  properties: {
    button_name: "submit",
    page: "checkout",
    value: 100,
  },
};
```

## Fields

| Field        | Type                  | Required           | Description                                                                      | Example                                                                       |
| ------------ | --------------------- | ------------------ | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `distinctId` | _string_              | :heavy_minus_sign: | Distinct ID for the user or entity (defaults to organization ID if not provided) |                                                                               |
| `event`      | _string_              | :heavy_check_mark: | Event name                                                                       | button_clicked                                                                |
| `properties` | Record<string, _any_> | :heavy_minus_sign: | Event properties as key-value pairs                                              | {<br/>"button_name": "submit",<br/>"page": "checkout",<br/>"value": 100<br/>} |
