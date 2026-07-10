# UpdateRemoteSessionClientRequest

## Example Usage

```typescript
import { UpdateRemoteSessionClientRequest } from "@gram/client/models/operations/updateremotesessionclient.js";

let value: UpdateRemoteSessionClientRequest = {
  updateRemoteSessionClientForm: {
    id: "66939744-034c-4ebd-9a4a-dbcbd394f994",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |
| `updateRemoteSessionClientForm`                                                                      | [components.UpdateRemoteSessionClientForm](../../models/components/updateremotesessionclientform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |