# AuthNumberSwitchScopesRequest

## Example Usage

```typescript
import { AuthNumberSwitchScopesRequest } from "@gram/sdk/models/operations";

let value: AuthNumberSwitchScopesRequest = {
  organizationId: "Veniam accusantium consectetur.",
  projectId: "Porro officiis.",
  xGramSession: "Est nobis consequatur.",
};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            | Example                                |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `organizationId`                       | *string*                               | :heavy_minus_sign:                     | The organization slug to switch scopes | Veniam accusantium consectetur.        |
| `projectId`                            | *string*                               | :heavy_minus_sign:                     | The project id to switch scopes too    | Porro officiis.                        |
| `xGramSession`                         | *string*                               | :heavy_minus_sign:                     | Session header                         | Est nobis consequatur.                 |