# SetToolsetUserSessionIssuerRequest

## Example Usage

```typescript
import { SetToolsetUserSessionIssuerRequest } from "@gram/client/models/operations/settoolsetusersessionissuer.js";

let value: SetToolsetUserSessionIssuerRequest = {
  slug: "<value>",
  setUserSessionIssuerRequestBody: {},
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `slug`                                                                                                   | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The slug of the toolset to link                                                                          |
| `gramSession`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Session header                                                                                           |
| `gramKey`                                                                                                | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | API Key header                                                                                           |
| `gramProject`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | project header                                                                                           |
| `setUserSessionIssuerRequestBody`                                                                        | [components.SetUserSessionIssuerRequestBody](../../models/components/setusersessionissuerrequestbody.md) | :heavy_check_mark:                                                                                       | N/A                                                                                                      |