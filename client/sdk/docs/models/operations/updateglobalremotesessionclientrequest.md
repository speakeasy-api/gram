# UpdateGlobalRemoteSessionClientRequest

## Example Usage

```typescript
import { UpdateGlobalRemoteSessionClientRequest } from "@gram/client/models/operations/updateglobalremotesessionclient.js";

let value: UpdateGlobalRemoteSessionClientRequest = {
  updateRemoteSessionClientForm: {
    id: "66939744-034c-4ebd-9a4a-dbcbd394f994",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `updateRemoteSessionClientForm`                                                                      | [components.UpdateRemoteSessionClientForm](../../models/components/updateremotesessionclientform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |