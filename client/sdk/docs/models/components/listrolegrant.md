# ListRoleGrant

## Example Usage

```typescript
import { ListRoleGrant } from "@gram/client/models/components/listrolegrant.js";

let value: ListRoleGrant = {
  scope: "project:blocked_write",
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `scope`                                                                        | [components.ListRoleGrantScope](../../models/components/listrolegrantscope.md) | :heavy_check_mark:                                                             | The scope slug this grant applies to.                                          |
| `selectors`                                                                    | [components.Selector](../../models/components/selector.md)[]                   | :heavy_minus_sign:                                                             | Selector constraints. Null means unrestricted.                                 |
| `subScopes`                                                                    | [components.SubScopes](../../models/components/subscopes.md)[]                 | :heavy_minus_sign:                                                             | The inherited scopes the primary scope grants.                                 |