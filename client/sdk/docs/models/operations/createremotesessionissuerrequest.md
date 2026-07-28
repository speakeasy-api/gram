# CreateRemoteSessionIssuerRequest

## Example Usage

```typescript
import { CreateRemoteSessionIssuerRequest } from "@gram/client/models/operations/createremotesessionissuer.js";

let value: CreateRemoteSessionIssuerRequest = {
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
| `gramKey`                       | _string_                                                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`                   | _string_                                                                                             | :heavy_minus_sign: | project header |
| `createRemoteSessionIssuerForm` | [components.CreateRemoteSessionIssuerForm](../../models/components/createremotesessionissuerform.md) | :heavy_check_mark: | N/A            |
