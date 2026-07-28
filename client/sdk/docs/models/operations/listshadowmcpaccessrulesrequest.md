# ListShadowMCPAccessRulesRequest

## Example Usage

```typescript
import { ListShadowMCPAccessRulesRequest } from "@gram/client/models/operations/listshadowmcpaccessrules.js";

let value: ListShadowMCPAccessRulesRequest = {};
```

## Fields

| Field         | Type                                                             | Required           | Description                          |
| ------------- | ---------------------------------------------------------------- | ------------------ | ------------------------------------ |
| `disposition` | [operations.Disposition](../../models/operations/disposition.md) | :heavy_minus_sign: | N/A                                  |
| `accessScope` | [operations.AccessScope](../../models/operations/accessscope.md) | :heavy_minus_sign: | N/A                                  |
| `projectId`   | _string_                                                         | :heavy_minus_sign: | N/A                                  |
| `limit`       | _number_                                                         | :heavy_minus_sign: | N/A                                  |
| `cursor`      | _string_                                                         | :heavy_minus_sign: | Cursor for the next page of results. |
| `gramSession` | _string_                                                         | :heavy_minus_sign: | Session header                       |
