# SecurityVariable

## Example Usage

```typescript
import { SecurityVariable } from "@gram/client/models/components/securityvariable.js";

let value: SecurityVariable = {
  envVariables: [],
  id: "<id>",
  inPlacement: "<value>",
  name: "<value>",
  scheme: "<value>",
};
```

## Fields

| Field          | Type         | Required           | Description                                                                        |
| -------------- | ------------ | ------------------ | ---------------------------------------------------------------------------------- |
| `bearerFormat` | _string_     | :heavy_minus_sign: | The bearer format                                                                  |
| `displayName`  | _string_     | :heavy_minus_sign: | User-friendly display name for the security variable (defaults to name if not set) |
| `envVariables` | _string_[]   | :heavy_check_mark: | The environment variables                                                          |
| `id`           | _string_     | :heavy_check_mark: | The unique identifier of the security variable                                     |
| `inPlacement`  | _string_     | :heavy_check_mark: | Where the security token is placed                                                 |
| `name`         | _string_     | :heavy_check_mark: | The name of the security scheme (actual header/parameter name)                     |
| `oauthFlows`   | _Uint8Array_ | :heavy_minus_sign: | The OAuth flows                                                                    |
| `oauthTypes`   | _string_[]   | :heavy_minus_sign: | The OAuth types                                                                    |
| `scheme`       | _string_     | :heavy_check_mark: | The security scheme                                                                |
| `type`         | _string_     | :heavy_minus_sign: | The type of security                                                               |
