# RevokeChatSessionRequest

## Example Usage

```typescript
import { RevokeChatSessionRequest } from "@gram/client/models/operations";

let value: RevokeChatSessionRequest = {
  token: "<value>",
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `token`                          | *string*                         | :heavy_check_mark:               | The chat session token to revoke |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramKey`                        | *string*                         | :heavy_minus_sign:               | API Key header                   |
| `gramProject`                    | *string*                         | :heavy_minus_sign:               | project header                   |