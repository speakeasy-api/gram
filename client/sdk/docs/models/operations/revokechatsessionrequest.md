# RevokeChatSessionRequest

## Example Usage

```typescript
import { RevokeChatSessionRequest } from "@gram/client/models/operations/revokechatsession.js";

let value: RevokeChatSessionRequest = {
  token: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                      |
| ------------- | -------- | ------------------ | -------------------------------- |
| `token`       | _string_ | :heavy_check_mark: | The chat session token to revoke |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                   |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                   |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                   |
