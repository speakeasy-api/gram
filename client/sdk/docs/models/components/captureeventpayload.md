# CaptureEventPayload

Payload for capturing a telemetry event

## Example Usage

```typescript
import { CaptureEventPayload } from "@gram/client/models/components";

let value: CaptureEventPayload = {
  event: "button_clicked",
  properties: {
    "button_name": "submit",
    "page": "checkout",
    "value": 100,
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      | Example                                                                          |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `distinctId`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | Distinct ID for the user or entity (defaults to organization ID if not provided) |                                                                                  |
| `event`                                                                          | *string*                                                                         | :heavy_check_mark:                                                               | Event name                                                                       | button_clicked                                                                   |
| `properties`                                                                     | Record<string, *any*>                                                            | :heavy_minus_sign:                                                               | Event properties as key-value pairs                                              | {<br/>"button_name": "submit",<br/>"page": "checkout",<br/>"value": 100<br/>}    |