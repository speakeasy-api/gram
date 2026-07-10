# UserSessionClient

A user_session_client (DCR'd MCP client). client_secret_hash is never returned.

## Example Usage

```typescript
import { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

let value: UserSessionClient = {
  clientId: "<id>",
  clientIdIssuedAt: new Date("2024-12-04T16:54:18.161Z"),
  clientName: "<value>",
  createdAt: new Date("2025-08-05T15:32:10.142Z"),
  id: "c3a3fad2-4423-4aeb-b5af-b3535f11d4f8",
  redirectUris: ["<value 1>", "<value 2>", "<value 3>"],
  updatedAt: new Date("2025-11-05T04:04:17.416Z"),
  userSessionIssuerId: "a88ecfeb-5ab9-4b1a-83d8-4b1a40145375",
};
```

## Fields

| Field                   | Type                                                                                          | Required           | Description                                 |
| ----------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------- |
| `clientId`              | _string_                                                                                      | :heavy_check_mark: | DCR-issued client_id.                       |
| `clientIdIssuedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                         |
| `clientName`            | _string_                                                                                      | :heavy_check_mark: | Display name from the registration request. |
| `clientSecretExpiresAt` | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Null when the secret does not expire.       |
| `createdAt`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                         |
| `id`                    | _string_                                                                                      | :heavy_check_mark: | The user_session_client id.                 |
| `redirectUris`          | _string_[]                                                                                    | :heavy_check_mark: | Validated on every /authorize.              |
| `updatedAt`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                         |
| `userSessionIssuerId`   | _string_                                                                                      | :heavy_check_mark: | The owning user_session_issuer id.          |
