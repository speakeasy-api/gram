# DeploymentLogEvent

## Example Usage

```typescript
import { DeploymentLogEvent } from "@gram/client/models/components";

let value: DeploymentLogEvent = {
  createdAt: "1715978530424",
  event: "<value>",
  id: "<id>",
  message: "<value>",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `createdAt`                        | *string*                           | :heavy_check_mark:                 | The creation date of the log event |
| `event`                            | *string*                           | :heavy_check_mark:                 | The type of event that occurred    |
| `id`                               | *string*                           | :heavy_check_mark:                 | The ID of the log event            |
| `message`                          | *string*                           | :heavy_check_mark:                 | The message of the log event       |