# CreateRemoteSessionClientRequest

## Example Usage

```typescript
import { CreateRemoteSessionClientRequest } from "@gram/client/models/operations/createremotesessionclient.js";

let value: CreateRemoteSessionClientRequest = {
  createRemoteSessionClientForm: {
    clientId: "<id>",
    remoteSessionIssuerId: "d8869670-8c02-49c2-9b3d-c61bcc49a8a6",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |
| `createRemoteSessionClientForm`                                                                      | [components.CreateRemoteSessionClientForm](../../models/components/createremotesessionclientform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |