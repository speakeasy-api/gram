# UserSessionFacetOption

## Example Usage

```typescript
import { UserSessionFacetOption } from "@gram/client/models/components/usersessionfacetoption.js";

let value: UserSessionFacetOption = {
  count: 60450,
  displayName: "Hilario.Moen66",
  value: "<value>",
};
```

## Fields

| Field                                    | Type                                     | Required                                 | Description                              |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `count`                                  | *number*                                 | :heavy_check_mark:                       | Number of sessions for this facet value. |
| `displayName`                            | *string*                                 | :heavy_check_mark:                       | The label shown for the facet value.     |
| `value`                                  | *string*                                 | :heavy_check_mark:                       | The facet value used for filtering.      |