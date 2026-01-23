# AddExternalOAuthServerRequest

## Example Usage

```typescript
import { AddExternalOAuthServerRequest } from "@gram/client/models/operations";

let value: AddExternalOAuthServerRequest = {
  slug: "<value>",
  addExternalOAuthServerRequestBody: {
    externalOauthServer: {
      metadata: "<value>",
      slug: "<value>",
    },
  },
};
```

## Fields

| Field                                                                                                        | Type                                                                                                         | Required                                                                                                     | Description                                                                                                  |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `slug`                                                                                                       | *string*                                                                                                     | :heavy_check_mark:                                                                                           | The slug of the toolset to update                                                                            |
| `gramSession`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | Session header                                                                                               |
| `gramKey`                                                                                                    | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | API Key header                                                                                               |
| `gramProject`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | project header                                                                                               |
| `addExternalOAuthServerRequestBody`                                                                          | [components.AddExternalOAuthServerRequestBody](../../models/components/addexternaloauthserverrequestbody.md) | :heavy_check_mark:                                                                                           | N/A                                                                                                          |