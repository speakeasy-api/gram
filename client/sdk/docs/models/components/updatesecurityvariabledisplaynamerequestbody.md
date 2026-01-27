# UpdateSecurityVariableDisplayNameRequestBody

## Example Usage

```typescript
import { UpdateSecurityVariableDisplayNameRequestBody } from "@gram/client/models/components";

let value: UpdateSecurityVariableDisplayNameRequestBody = {
  displayName: "Jenifer.Wilkinson",
  securityKey: "<value>",
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                                                   | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `displayName`                                                                           | *string*                                                                                | :heavy_check_mark:                                                                      | The user-friendly display name. Set to empty string to clear and use the original name. |
| `securityKey`                                                                           | *string*                                                                                | :heavy_check_mark:                                                                      | The security scheme key (e.g., 'BearerAuth', 'ApiKeyAuth') from the OpenAPI spec        |
| `toolsetSlug`                                                                           | *string*                                                                                | :heavy_check_mark:                                                                      | The slug of the toolset containing the security variable                                |