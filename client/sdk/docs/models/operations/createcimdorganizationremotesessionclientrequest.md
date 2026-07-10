# CreateCimdOrganizationRemoteSessionClientRequest

## Example Usage

```typescript
import { CreateCimdOrganizationRemoteSessionClientRequest } from "@gram/client/models/operations/createcimdorganizationremotesessionclient.js";

let value: CreateCimdOrganizationRemoteSessionClientRequest = {
  createCimdOrganizationRemoteSessionClientForm: {
    remoteSessionIssuerId: "0fdf7243-4302-4860-96b7-6dd0c987adf6",
  },
};
```

## Fields

| Field                                                                                                                                | Type                                                                                                                                 | Required                                                                                                                             | Description                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                                                        | *string*                                                                                                                             | :heavy_minus_sign:                                                                                                                   | Session header                                                                                                                       |
| `gramKey`                                                                                                                            | *string*                                                                                                                             | :heavy_minus_sign:                                                                                                                   | API Key header                                                                                                                       |
| `createCimdOrganizationRemoteSessionClientForm`                                                                                      | [components.CreateCimdOrganizationRemoteSessionClientForm](../../models/components/createcimdorganizationremotesessionclientform.md) | :heavy_check_mark:                                                                                                                   | N/A                                                                                                                                  |