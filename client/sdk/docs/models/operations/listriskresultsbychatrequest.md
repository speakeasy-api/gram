# ListRiskResultsByChatRequest

## Example Usage

```typescript
import { ListRiskResultsByChatRequest } from "@gram/client/models/operations/listriskresultsbychat.js";

let value: ListRiskResultsByChatRequest = {};
```

## Fields

| Field                                         | Type                                          | Required                                      | Description                                   |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `cursor`                                      | *string*                                      | :heavy_minus_sign:                            | Cursor to fetch the next page of results.     |
| `limit`                                       | *number*                                      | :heavy_minus_sign:                            | Maximum number of results to return per page. |
| `gramKey`                                     | *string*                                      | :heavy_minus_sign:                            | API Key header                                |
| `gramSession`                                 | *string*                                      | :heavy_minus_sign:                            | Session header                                |
| `gramProject`                                 | *string*                                      | :heavy_minus_sign:                            | project header                                |