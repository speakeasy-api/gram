# UpdateUserSessionIssuerRequest

## Example Usage

```typescript
import { UpdateUserSessionIssuerRequest } from "@gram/client/models/operations/updateusersessionissuer.js";

let value: UpdateUserSessionIssuerRequest = {
  updateUserSessionIssuerForm: {
    id: "3aca7f18-d4a2-4291-b277-121e8dc85648",
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |
| `updateUserSessionIssuerForm`                                                                    | [components.UpdateUserSessionIssuerForm](../../models/components/updateusersessionissuerform.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |