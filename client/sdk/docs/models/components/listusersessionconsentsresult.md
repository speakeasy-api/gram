# ListUserSessionConsentsResult

Result type for listing user_session_consents.

## Example Usage

```typescript
import { ListUserSessionConsentsResult } from "@gram/client/models/components/listusersessionconsentsresult.js";

let value: ListUserSessionConsentsResult = {
  items: [
    {
      consentedAt: new Date("2026-05-06T12:21:15.259Z"),
      createdAt: new Date("2026-03-05T02:30:34.466Z"),
      id: "3f3f3776-540b-4ec9-9808-8b049adbf1c6",
      remoteSetHash: "<value>",
      subjectUrn: "<value>",
      updatedAt: new Date("2026-06-21T06:40:28.157Z"),
      userSessionClientId: "8c08a879-7b85-40c1-9ada-c99aa7d3c9ce",
    },
  ],
};
```

## Fields

| Field        | Type                                                                             | Required           | Description                                     |
| ------------ | -------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------- |
| `items`      | [components.UserSessionConsent](../../models/components/usersessionconsent.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                                         | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
