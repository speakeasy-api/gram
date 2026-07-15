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

| Field             | Type     | Required           | Description                                      |
| ----------------- | -------- | ------------------ | ------------------------------------------------ |
| `registryId`      | _string_ | :heavy_check_mark: | ID of the registry                               |
| `serverSpecifier` | _string_ | :heavy_check_mark: | Server specifier (e.g., 'io.github.user/server') |
| `gramSession`     | _string_ | :heavy_minus_sign: | Session header                                   |
| `gramKey`         | _string_ | :heavy_minus_sign: | API Key header                                   |
| `gramProject`     | _string_ | :heavy_minus_sign: | project header                                   |
