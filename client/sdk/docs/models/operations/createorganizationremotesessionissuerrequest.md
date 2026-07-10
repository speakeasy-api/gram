# CreateOrganizationRemoteSessionIssuerRequest

## Example Usage

```typescript
import { CreateOrganizationRemoteSessionIssuerRequest } from "@gram/client/models/operations/createorganizationremotesessionissuer.js";

let value: CreateOrganizationRemoteSessionIssuerRequest = {
  createIssuerRequestBody: {
    issuer: "diners_club",
    slug: "<value>",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `gramKey`                                                                                | *string*                                                                                 | :heavy_minus_sign:                                                                       | API Key header                                                                           |
| `createIssuerRequestBody`                                                                | [components.CreateIssuerRequestBody](../../models/components/createissuerrequestbody.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |