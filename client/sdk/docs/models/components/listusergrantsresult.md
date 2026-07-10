# ListUserGrantsResult

## Example Usage

```typescript
import { ListUserGrantsResult } from "@gram/client/models/components/listusergrantsresult.js";

let value: ListUserGrantsResult = {
  grants: [],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `grants`                                                               | [components.ListRoleGrant](../../models/components/listrolegrant.md)[] | :heavy_check_mark:                                                     | The user's effective grants in this organization.                      |