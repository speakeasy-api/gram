# HookNotificationData

Local agent notification payload.

## Example Usage

```typescript
import { HookNotificationData } from "@gram/client/models/components/hooknotificationdata.js";

let value: HookNotificationData = {};
```

## Fields

| Field                 | Type                  | Required              | Description           |
| --------------------- | --------------------- | --------------------- | --------------------- |
| `message`             | *string*              | :heavy_minus_sign:    | Notification message. |
| `title`               | *string*              | :heavy_minus_sign:    | Notification title.   |
| `type`                | *string*              | :heavy_minus_sign:    | Notification type.    |