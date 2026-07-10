# RoleGrant

## Example Usage

```typescript
import { RoleGrant } from "@gram/client/models/components/rolegrant.js";

let value: RoleGrant = {
  scope: "environment:write",
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `scope`                                                      | [components.Scope](../../models/components/scope.md)         | :heavy_check_mark:                                           | The scope slug this grant applies to.                        |
| `selectors`                                                  | [components.Selector](../../models/components/selector.md)[] | :heavy_minus_sign:                                           | Selector constraints. Null means unrestricted.               |