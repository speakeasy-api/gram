# ListUserSessionConsentsRequest

## Example Usage

```typescript
import { ListUserSessionConsentsRequest } from "@gram/client/models/operations/listusersessionconsents.js";

let value: ListUserSessionConsentsRequest = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `subjectUrn`                                                           | *string*                                                               | :heavy_minus_sign:                                                     | Filter by subject URN.                                                 |
| `userSessionClientId`                                                  | *string*                                                               | :heavy_minus_sign:                                                     | Filter by user_session_client id.                                      |
| `userSessionIssuerId`                                                  | *string*                                                               | :heavy_minus_sign:                                                     | Filter by user_session_issuer id (joins through user_session_clients). |
| `cursor`                                                               | *string*                                                               | :heavy_minus_sign:                                                     | Pagination cursor: id of the last item from the previous page.         |
| `limit`                                                                | *number*                                                               | :heavy_minus_sign:                                                     | Page size (default 50, max 100).                                       |
| `gramSession`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Session header                                                         |
| `gramKey`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | API Key header                                                         |
| `gramProject`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | project header                                                         |