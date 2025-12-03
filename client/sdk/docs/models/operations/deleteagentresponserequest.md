# DeleteAgentResponseRequest

## Example Usage

```typescript
import { DeleteAgentResponseRequest } from "@gram/client/models/operations";

let value: DeleteAgentResponseRequest = {
  responseId: "<id>",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `responseId`                       | *string*                           | :heavy_check_mark:                 | The ID of the response to retrieve |
| `gramKey`                          | *string*                           | :heavy_minus_sign:                 | API Key header                     |
| `gramProject`                      | *string*                           | :heavy_minus_sign:                 | project header                     |