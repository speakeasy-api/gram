# CreateOrganizationRemoteSessionClientRequest

## Example Usage

```typescript
import { CreateOrganizationRemoteSessionClientRequest } from "@gram/client/models/operations/createorganizationremotesessionclient.js";

let value: CreateOrganizationRemoteSessionClientRequest = {
  createOrganizationRemoteSessionClientForm: {
    clientId: "<id>",
    remoteSessionIssuerId: "2f50b50f-6428-45e5-ab38-899bd2ee7dab",
  },
};
```

## Fields

| Field                                       | Type                                                                                                                         | Required           | Description    |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                               | _string_                                                                                                                     | :heavy_minus_sign: | Session header |
| `gramKey`                                   | _string_                                                                                                                     | :heavy_minus_sign: | API Key header |
| `createOrganizationRemoteSessionClientForm` | [components.CreateOrganizationRemoteSessionClientForm](../../models/components/createorganizationremotesessionclientform.md) | :heavy_check_mark: | N/A            |
