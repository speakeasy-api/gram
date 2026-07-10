# DeploymentLogEvent

## Example Usage

```typescript
import { DeploymentLogEvent } from "@gram/client/models/components/deploymentlogevent.js";

let value: DeploymentLogEvent = {
  createdAt: "1715978530424",
  event: "<value>",
  id: "<id>",
  message: "<value>",
};
```

## Fields

| Field            | Type     | Required           | Description                                 |
| ---------------- | -------- | ------------------ | ------------------------------------------- |
| `attachmentId`   | _string_ | :heavy_minus_sign: | The ID of the asset tied to the log event   |
| `attachmentType` | _string_ | :heavy_minus_sign: | The type of the asset tied to the log event |
| `createdAt`      | _string_ | :heavy_check_mark: | The creation date of the log event          |
| `event`          | _string_ | :heavy_check_mark: | The type of event that occurred             |
| `id`             | _string_ | :heavy_check_mark: | The ID of the log event                     |
| `message`        | _string_ | :heavy_check_mark: | The message of the log event                |
