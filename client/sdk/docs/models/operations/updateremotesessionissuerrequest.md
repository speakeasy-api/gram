# UpdateRemoteSessionIssuerRequest

## Example Usage

```typescript
import { UpdateRemoteSessionIssuerRequest } from "@gram/client/models/operations/updateremotesessionissuer.js";

let value: UpdateRemoteSessionIssuerRequest = {
  updateRemoteSessionIssuerForm: {
    id: "ba2aa96b-b6be-43d6-9fa1-a6d79b187cc3",
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header |
| `gramKey`                       | _string_                                                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`                   | _string_                                                                                             | :heavy_minus_sign: | project header |
| `updateRemoteSessionIssuerForm` | [components.UpdateRemoteSessionIssuerForm](../../models/components/updateremotesessionissuerform.md) | :heavy_check_mark: | N/A            |
