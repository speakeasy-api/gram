# SwitchAuthScopesRequest

## Example Usage

```typescript
import { SwitchAuthScopesRequest } from "@gram/client/models/operations";

let value: SwitchAuthScopesRequest = {};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `organizationId`                       | *string*                               | :heavy_minus_sign:                     | The organization slug to switch scopes |
| `projectId`                            | *string*                               | :heavy_minus_sign:                     | The project id to switch scopes too    |
| `gramSession`                          | *string*                               | :heavy_minus_sign:                     | Session header                         |