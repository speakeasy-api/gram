# ListShadowMCPAccessRulesRequest

## Example Usage

```typescript
import { ListShadowMCPAccessRulesRequest } from "@gram/client/models/operations/listshadowmcpaccessrules.js";

let value: ListShadowMCPAccessRulesRequest = {};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `disposition`                                                    | [operations.Disposition](../../models/operations/disposition.md) | :heavy_minus_sign:                                               | N/A                                                              |
| `accessScope`                                                    | [operations.AccessScope](../../models/operations/accessscope.md) | :heavy_minus_sign:                                               | N/A                                                              |
| `projectId`                                                      | *string*                                                         | :heavy_minus_sign:                                               | N/A                                                              |
| `limit`                                                          | *number*                                                         | :heavy_minus_sign:                                               | N/A                                                              |
| `cursor`                                                         | *string*                                                         | :heavy_minus_sign:                                               | Cursor for the next page of results.                             |
| `gramSession`                                                    | *string*                                                         | :heavy_minus_sign:                                               | Session header                                                   |