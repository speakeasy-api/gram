# ListShadowMCPAccessRulesResult

## Example Usage

```typescript
import { ListShadowMCPAccessRulesResult } from "@gram/client/models/components/listshadowmcpaccessrulesresult.js";

let value: ListShadowMCPAccessRulesResult = {
  rules: [],
};
```

## Fields

| Field        | Type                                                                               | Required           | Description                          |
| ------------ | ---------------------------------------------------------------------------------- | ------------------ | ------------------------------------ |
| `nextCursor` | _string_                                                                           | :heavy_minus_sign: | Cursor for the next page of results. |
| `rules`      | [components.ShadowMCPAccessRule](../../models/components/shadowmcpaccessrule.md)[] | :heavy_check_mark: | N/A                                  |
