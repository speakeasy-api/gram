# ListRemoteSessionsRequest

## Example Usage

```typescript
import { ListRemoteSessionsRequest } from "@gram/client/models/operations/listremotesessions.js";

let value: ListRemoteSessionsRequest = {};
```

## Fields

| Field                   | Type     | Required           | Description                         |
| ----------------------- | -------- | ------------------ | ----------------------------------- |
| `subjectUrn`            | _string_ | :heavy_minus_sign: | Exact-match filter on subject URN.  |
| `remoteSessionClientId` | _string_ | :heavy_minus_sign: | Filter by remote_session_client id. |
| `cursor`                | _string_ | :heavy_minus_sign: | Pagination cursor.                  |
| `limit`                 | _number_ | :heavy_minus_sign: | Page size (default 50, max 100).    |
| `gramSession`           | _string_ | :heavy_minus_sign: | Session header                      |
| `gramKey`               | _string_ | :heavy_minus_sign: | API Key header                      |
| `gramProject`           | _string_ | :heavy_minus_sign: | project header                      |
