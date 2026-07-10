# GetMCPServerDetailsRequest

## Example Usage

```typescript
import { GetMCPServerDetailsRequest } from "@gram/client/models/operations/getmcpserverdetails.js";

let value: GetMCPServerDetailsRequest = {
  registryId: "3d69f148-236f-42fb-9f62-3f4ce2e8c936",
  serverSpecifier: "<value>",
};
```

## Fields

| Field                                            | Type                                             | Required                                         | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ |
| `registryId`                                     | *string*                                         | :heavy_check_mark:                               | ID of the registry                               |
| `serverSpecifier`                                | *string*                                         | :heavy_check_mark:                               | Server specifier (e.g., 'io.github.user/server') |
| `gramSession`                                    | *string*                                         | :heavy_minus_sign:                               | Session header                                   |
| `gramKey`                                        | *string*                                         | :heavy_minus_sign:                               | API Key header                                   |
| `gramProject`                                    | *string*                                         | :heavy_minus_sign:                               | project header                                   |