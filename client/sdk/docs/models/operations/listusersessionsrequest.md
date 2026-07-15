# ListUserSessionsRequest

## Example Usage

```typescript
import { ListUserSessionsRequest } from "@gram/client/models/operations/listusersessions.js";

let value: ListUserSessionsRequest = {};
```

## Fields

| Field                 | Type                                                                                                       | Required           | Description                                                    |
| --------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------- |
| `subjectUrn`          | _string_                                                                                                   | :heavy_minus_sign: | Exact-match filter on subject URN.                             |
| `userSessionIssuerId` | _string_                                                                                                   | :heavy_minus_sign: | Filter by user_session_issuer id.                              |
| `status`              | [operations.ListUserSessionsQueryParamStatus](../../models/operations/listusersessionsqueryparamstatus.md) | :heavy_minus_sign: | Filter by session status.                                      |
| `clientId`            | _string_                                                                                                   | :heavy_minus_sign: | Filter by the connecting client id.                            |
| `cursor`              | _string_                                                                                                   | :heavy_minus_sign: | Pagination cursor: id of the last item from the previous page. |
| `limit`               | _number_                                                                                                   | :heavy_minus_sign: | Page size (default 50, max 100).                               |
| `gramSession`         | _string_                                                                                                   | :heavy_minus_sign: | Session header                                                 |
| `gramKey`             | _string_                                                                                                   | :heavy_minus_sign: | API Key header                                                 |
| `gramProject`         | _string_                                                                                                   | :heavy_minus_sign: | project header                                                 |
