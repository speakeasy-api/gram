# AuthNumberSwitchScopesRequest

## Example Usage

```typescript
import { AuthNumberSwitchScopesRequest } from "@gram/sdk/models/operations";

let value: AuthNumberSwitchScopesRequest = {};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `organizationId`                       | *string*                               | :heavy_minus_sign:                     | The organization slug to switch scopes |
| `projectId`                            | *string*                               | :heavy_minus_sign:                     | The project id to switch scopes too    |
| `gramSession`                          | *string*                               | :heavy_minus_sign:                     | Session header                         |