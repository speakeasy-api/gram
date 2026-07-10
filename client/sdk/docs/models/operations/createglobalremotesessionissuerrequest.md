# CreateGlobalRemoteSessionIssuerRequest

## Example Usage

```typescript
import { CreateGlobalRemoteSessionIssuerRequest } from "@gram/client/models/operations/createglobalremotesessionissuer.js";

let value: CreateGlobalRemoteSessionIssuerRequest = {
  createRemoteSessionIssuerForm: {
    issuer: "american_express",
    slug: "<value>",
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header |
| `createRemoteSessionIssuerForm` | [components.CreateRemoteSessionIssuerForm](../../models/components/createremotesessionissuerform.md) | :heavy_check_mark: | N/A            |
