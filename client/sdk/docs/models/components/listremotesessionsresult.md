# ListRemoteSessionsResult

Result type for listing remote_sessions.

## Example Usage

```typescript
import { ListRemoteSessionsResult } from "@gram/client/models/components/listremotesessionsresult.js";

let value: ListRemoteSessionsResult = {
  items: [
    {
      accessExpiresAt: new Date("2025-11-16T07:31:19.085Z"),
      createdAt: new Date("2024-10-17T00:32:13.001Z"),
      hasRefreshToken: true,
      id: "657cfcc9-7c82-4a7a-b07b-a734d508cce2",
      remoteSessionClientId: "7d922b59-42e0-42d2-9dbf-d0e030d9d0aa",
      scopes: [],
      subjectUrn: "<value>",
      updatedAt: new Date("2026-05-19T19:04:54.531Z"),
      userSessionIssuerId: "b1a16695-4e94-4b0f-93ee-c4c9da2e51ec",
    },
  ],
};
```

## Fields

| Field        | Type                                                                   | Required           | Description                                     |
| ------------ | ---------------------------------------------------------------------- | ------------------ | ----------------------------------------------- |
| `items`      | [components.RemoteSession](../../models/components/remotesession.md)[] | :heavy_check_mark: | N/A                                             |
| `nextCursor` | _string_                                                               | :heavy_minus_sign: | Cursor for the next page; empty when exhausted. |
