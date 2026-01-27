# SecurityVariable

## Example Usage

```typescript
import { SecurityVariable } from "@gram/client/models/components";

let value: SecurityVariable = {
  envVariables: [],
  id: "<id>",
  inPlacement: "<value>",
  name: "<value>",
  scheme: "<value>",
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `bearerFormat`                                                                     | *string*                                                                           | :heavy_minus_sign:                                                                 | The bearer format                                                                  |
| `displayName`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | User-friendly display name for the security variable (defaults to name if not set) |
| `envVariables`                                                                     | *string*[]                                                                         | :heavy_check_mark:                                                                 | The environment variables                                                          |
| `id`                                                                               | *string*                                                                           | :heavy_check_mark:                                                                 | The unique identifier of the security variable                                     |
| `inPlacement`                                                                      | *string*                                                                           | :heavy_check_mark:                                                                 | Where the security token is placed                                                 |
| `name`                                                                             | *string*                                                                           | :heavy_check_mark:                                                                 | The name of the security scheme (actual header/parameter name)                     |
| `oauthFlows`                                                                       | *Uint8Array*                                                                       | :heavy_minus_sign:                                                                 | The OAuth flows                                                                    |
| `oauthTypes`                                                                       | *string*[]                                                                         | :heavy_minus_sign:                                                                 | The OAuth types                                                                    |
| `scheme`                                                                           | *string*                                                                           | :heavy_check_mark:                                                                 | The security scheme                                                                |
| `type`                                                                             | *string*                                                                           | :heavy_minus_sign:                                                                 | The type of security                                                               |