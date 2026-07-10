# UserSession

An issued user_session record. refresh_token_hash is never returned.

## Example Usage

```typescript
import { UserSession } from "@gram/client/models/components/usersession.js";

let value: UserSession = {
  createdAt: new Date("2024-04-04T12:16:31.656Z"),
  expiresAt: new Date("2026-08-12T12:26:04.103Z"),
  id: "2c736934-52d2-4079-a6d5-557ab70ea9eb",
  issuerSlug: "<value>",
  jti: "<value>",
  refreshExpiresAt: new Date("2024-09-09T08:02:18.123Z"),
  subjectType: "<value>",
  subjectUrn: "<value>",
  updatedAt: new Date("2025-09-03T17:09:49.050Z"),
  userSessionIssuerId: "fc9d6692-f169-4e4b-8926-7681244dc376",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `clientName`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Name of the MCP client that established the session, if known.                                |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `expiresAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Terminal session expiry; ceiling on refresh_expires_at.                                       |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The user_session id.                                                                          |
| `issuerSlug`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | Slug of the user_session_issuer that gated this session.                                      |
| `jti`                                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | Current access-token JTI; used by the revocation path.                                        |
| `refreshExpiresAt`                                                                            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Next refresh deadline.                                                                        |
| `revokedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | When the session was revoked, if it has been.                                                 |
| `subjectDisplayName`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | Resolved human-readable name of the subject, if known.                                        |
| `subjectType`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Subject kind: 'user', 'apikey', or 'anonymous'.                                               |
| `subjectUrn`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The session's subject URN (user:<id> \| apikey:<uuid> \| anonymous:<mcp-session-id>).         |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `userSessionIssuerId`                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The issuing user_session_issuer id.                                                           |