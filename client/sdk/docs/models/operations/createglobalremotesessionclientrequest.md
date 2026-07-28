# CreateGlobalRemoteSessionClientRequest

## Example Usage

```typescript
import { CreateGlobalRemoteSessionClientRequest } from "@gram/client/models/operations/createglobalremotesessionclient.js";

let value: CreateGlobalRemoteSessionClientRequest = {
  createGlobalRemoteSessionClientForm: {
    clientId: "<id>",
    remoteSessionIssuerId: "c23c45a2-c705-484b-a0a1-bb3636d8b828",
  },
};
```

## Fields

| Field                                 | Type                                                                                                             | Required           | Description    |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                         | _string_                                                                                                         | :heavy_minus_sign: | Session header |
| `createGlobalRemoteSessionClientForm` | [components.CreateGlobalRemoteSessionClientForm](../../models/components/createglobalremotesessionclientform.md) | :heavy_check_mark: | N/A            |
