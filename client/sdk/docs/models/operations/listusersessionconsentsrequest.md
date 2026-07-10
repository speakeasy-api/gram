# ListUserSessionConsentsRequest

## Example Usage

```typescript
import { ListUserSessionConsentsRequest } from "@gram/client/models/operations/listusersessionconsents.js";

let value: ListUserSessionConsentsRequest = {};
```

## Fields

| Field                 | Type     | Required           | Description                                                            |
| --------------------- | -------- | ------------------ | ---------------------------------------------------------------------- |
| `subjectUrn`          | _string_ | :heavy_minus_sign: | Filter by subject URN.                                                 |
| `userSessionClientId` | _string_ | :heavy_minus_sign: | Filter by user_session_client id.                                      |
| `userSessionIssuerId` | _string_ | :heavy_minus_sign: | Filter by user_session_issuer id (joins through user_session_clients). |
| `cursor`              | _string_ | :heavy_minus_sign: | Pagination cursor: id of the last item from the previous page.         |
| `limit`               | _number_ | :heavy_minus_sign: | Page size (default 50, max 100).                                       |
| `gramSession`         | _string_ | :heavy_minus_sign: | Session header                                                         |
| `gramKey`             | _string_ | :heavy_minus_sign: | API Key header                                                         |
| `gramProject`         | _string_ | :heavy_minus_sign: | project header                                                         |
