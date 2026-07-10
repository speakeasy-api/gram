# AddExternalOAuthServerRequest

## Example Usage

```typescript
import { AddExternalOAuthServerRequest } from "@gram/client/models/operations/addexternaloauthserver.js";

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

| Field                               | Type                                                                                                         | Required           | Description                       |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | --------------------------------- |
| `slug`                              | _string_                                                                                                     | :heavy_check_mark: | The slug of the toolset to update |
| `gramSession`                       | _string_                                                                                                     | :heavy_minus_sign: | Session header                    |
| `gramKey`                           | _string_                                                                                                     | :heavy_minus_sign: | API Key header                    |
| `gramProject`                       | _string_                                                                                                     | :heavy_minus_sign: | project header                    |
| `addExternalOAuthServerRequestBody` | [components.AddExternalOAuthServerRequestBody](../../models/components/addexternaloauthserverrequestbody.md) | :heavy_check_mark: | N/A                               |
