# KickoffMessageRequestBody

## Example Usage

```typescript
import { KickoffMessageRequestBody } from "@gram/client/models/components";

let value: KickoffMessageRequestBody = {
  assistantId: "4e9f7062-be32-463c-b99a-55a9eb1ffa67",
  correlationId: "<id>",
};
```

## Fields

| Field           | Type     | Required           | Description                                                                                                                                        |
| --------------- | -------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assistantId`   | _string_ | :heavy_check_mark: | The assistant to greet from.                                                                                                                       |
| `correlationId` | _string_ | :heavy_check_mark: | Conversation key to greet within — pass the same value used for sendMessage so the assistant greets inside the existing thread (and can recap it). |
