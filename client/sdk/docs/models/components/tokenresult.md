# TokenResult

A minted per-user API key for the device agent.

## Example Usage

```typescript
import { TokenResult } from "@gram/client/models/components";

let value: TokenResult = {
  accessToken: "<value>",
  expiresIn: 511800,
  refreshToken: "<value>",
  userEmail: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                                                                                                                                                         |
| -------------- | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `accessToken`  | _string_ | :heavy_check_mark: | The raw per-user API key (carries the `agent_user` scope). Returned exactly once; store it securely. Presented as the Gram-Key on downstream user-scoped endpoints. |
| `expiresIn`    | _number_ | :heavy_check_mark: | Always zero. The minted key has no expiry (api_keys has no TTL).                                                                                                    |
| `refreshToken` | _string_ | :heavy_check_mark: | Always empty. The minted key is long-lived and does not refresh; its lifecycle lever is revocation.                                                                 |
| `userEmail`    | _string_ | :heavy_check_mark: | Email the key was minted for.                                                                                                                                       |
