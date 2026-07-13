# TelemetryWindowPayload

An org-scoped time window, optionally narrowed to one project

## Example Usage

```typescript
import { TelemetryWindowPayload } from "@gram/client/models/components/telemetrywindowpayload.js";

let value: TelemetryWindowPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-26T10:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | Optional project to scope to; defaults to every project in the organization.                  |                                                                                               |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-26T10:00:00Z                                                                          |