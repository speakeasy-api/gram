# SecurityVariable

## Example Usage

```typescript
import { SecurityVariable } from "@gram/client/models/components";

let value: SecurityVariable = {
  envVariables: [],
  inPlacement: "<value>",
  name: "<value>",
  scheme: "<value>",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `bearerFormat`                     | *string*                           | :heavy_minus_sign:                 | The bearer format                  |
| `envVariables`                     | *string*[]                         | :heavy_check_mark:                 | The environment variables          |
| `inPlacement`                      | *string*                           | :heavy_check_mark:                 | Where the security token is placed |
| `name`                             | *string*                           | :heavy_check_mark:                 | The name of the security scheme    |
| `oauthFlows`                       | *Uint8Array*                       | :heavy_minus_sign:                 | The OAuth flows                    |
| `oauthTypes`                       | *string*[]                         | :heavy_minus_sign:                 | The OAuth types                    |
| `scheme`                           | *string*                           | :heavy_check_mark:                 | The security scheme                |
| `type`                             | *string*                           | :heavy_minus_sign:                 | The type of security               |