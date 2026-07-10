# UserSessionConsent

A user_session_consent record. Per-client (not per-issuer) consent.

## Example Usage

```typescript
import { UserSessionConsent } from "@gram/client/models/components/usersessionconsent.js";

let value: UserSessionConsent = {
  consentedAt: new Date("2026-11-17T22:38:58.572Z"),
  createdAt: new Date("2025-09-11T10:32:22.556Z"),
  id: "65ddd567-041b-4693-9e1f-a8d9720920e5",
  remoteSetHash: "<value>",
  subjectUrn: "<value>",
  updatedAt: new Date("2024-02-13T01:12:35.102Z"),
  userSessionClientId: "8b7f56aa-967c-4452-8714-ef273be5389f",
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `consentedAt`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)          | :heavy_check_mark:                                                                                     | N/A                                                                                                    |
| `createdAt`                                                                                            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)          | :heavy_check_mark:                                                                                     | N/A                                                                                                    |
| `id`                                                                                                   | *string*                                                                                               | :heavy_check_mark:                                                                                     | The user_session_consent id.                                                                           |
| `remoteSetHash`                                                                                        | *string*                                                                                               | :heavy_check_mark:                                                                                     | SHA-256 of the sorted list of remote_session_issuer ids on the client's owning issuer at consent time. |
| `subjectUrn`                                                                                           | *string*                                                                                               | :heavy_check_mark:                                                                                     | The consenting subject URN (user:<id> \| apikey:<uuid> \| anonymous:<mcp-session-id>).                 |
| `updatedAt`                                                                                            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)          | :heavy_check_mark:                                                                                     | N/A                                                                                                    |
| `userSessionClientId`                                                                                  | *string*                                                                                               | :heavy_check_mark:                                                                                     | The user_session_client this consent binds to.                                                         |