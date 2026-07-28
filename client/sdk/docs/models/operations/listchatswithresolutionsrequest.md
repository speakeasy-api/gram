# ListChatsWithResolutionsRequest

## Example Usage

```typescript
import { ListChatsWithResolutionsRequest } from "@gram/client/models/operations";

let value: ListChatsWithResolutionsRequest = {};
```

## Fields

| Field              | Type                                                                                          | Required           | Description                                                                        |
| ------------------ | --------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------- |
| `search`           | _string_                                                                                      | :heavy_minus_sign: | Search query (searches chat ID, user ID, and title)                                |
| `externalUserId`   | _string_                                                                                      | :heavy_minus_sign: | Filter by external user ID                                                         |
| `assistantId`      | _string_                                                                                      | :heavy_minus_sign: | Filter to chats produced by this assistant                                         |
| `resolutionStatus` | _string_                                                                                      | :heavy_minus_sign: | Filter by resolution status                                                        |
| `hasRisk`          | [operations.HasRisk](../../models/operations/hasrisk.md)                                      | :heavy_minus_sign: | Filter by whether chat has risk findings: 'true', 'false', or empty for no filter. |
| `from`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Filter chats created after this timestamp (ISO 8601)                               |
| `to`               | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Filter chats created before this timestamp (ISO 8601)                              |
| `limit`            | _number_                                                                                      | :heavy_minus_sign: | Number of results per page                                                         |
| `offset`           | _number_                                                                                      | :heavy_minus_sign: | Pagination offset                                                                  |
| `sortBy`           | [operations.SortBy](../../models/operations/sortby.md)                                        | :heavy_minus_sign: | Field to sort by                                                                   |
| `sortOrder`        | [operations.SortOrder](../../models/operations/sortorder.md)                                  | :heavy_minus_sign: | Sort order                                                                         |
| `gramSession`      | _string_                                                                                      | :heavy_minus_sign: | Session header                                                                     |
| `gramProject`      | _string_                                                                                      | :heavy_minus_sign: | project header                                                                     |
| `gramChatSession`  | _string_                                                                                      | :heavy_minus_sign: | Chat Sessions token header                                                         |
