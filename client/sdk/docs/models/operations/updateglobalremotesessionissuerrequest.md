# UpdateGlobalRemoteSessionIssuerRequest

## Example Usage

```typescript
import { UpdateGlobalRemoteSessionIssuerRequest } from "@gram/client/models/operations/updateglobalremotesessionissuer.js";

let value: UpdateGlobalRemoteSessionIssuerRequest = {
  updateRemoteSessionIssuerForm: {
    id: "ba2aa96b-b6be-43d6-9fa1-a6d79b187cc3",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `updateRemoteSessionIssuerForm`                                                                      | [components.UpdateRemoteSessionIssuerForm](../../models/components/updateremotesessionissuerform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |