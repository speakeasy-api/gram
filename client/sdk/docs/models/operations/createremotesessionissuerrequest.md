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

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |
| `createRemoteSessionIssuerForm`                                                                      | [components.CreateRemoteSessionIssuerForm](../../models/components/createremotesessionissuerform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |