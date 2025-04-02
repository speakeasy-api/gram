# AuthNumberSwitchScopesRequest

## Example Usage

```typescript
import { AuthNumberSwitchScopesRequest } from "@gram/sdk/models/operations";

let value: AuthNumberSwitchScopesRequest = {
  organizationId: "Recusandae odio omnis.",
  projectId: "Necessitatibus accusamus repudiandae iste non voluptas.",
  gramSession: "Non odit laudantium eligendi quia sed.",
};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             | Example                                                 |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `organizationId`                                        | *string*                                                | :heavy_minus_sign:                                      | The organization slug to switch scopes                  | Recusandae odio omnis.                                  |
| `projectId`                                             | *string*                                                | :heavy_minus_sign:                                      | The project id to switch scopes too                     | Necessitatibus accusamus repudiandae iste non voluptas. |
| `gramSession`                                           | *string*                                                | :heavy_minus_sign:                                      | Session header                                          | Non odit laudantium eligendi quia sed.                  |