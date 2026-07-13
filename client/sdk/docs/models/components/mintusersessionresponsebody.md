# MintUserSessionResponseBody

## Example Usage

```typescript
import { MintUserSessionResponseBody } from "@gram/client/models/components/mintusersessionresponsebody.js";

let value: MintUserSessionResponseBody = {
  accessToken: "<value>",
  expiresIn: 105192,
};
```

## Fields

| Field         | Type     | Required           | Description                                                                                                                       |
| ------------- | -------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| `accessToken` | _string_ | :heavy_check_mark: | The minted user-session JWT. Send as `Authorization: Bearer` on MCP requests to the bound /mcp/{slug} (or /x/mcp/{slug}) surface. |
| `expiresIn`   | _number_ | :heavy_check_mark: | Lifetime of the access token in seconds.                                                                                          |
