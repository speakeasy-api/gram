# SwitchAuthScopesRequest

## Example Usage

```typescript
import { SwitchAuthScopesRequest } from "@gram/client/models/operations/switchauthscopes.js";

let value: SwitchAuthScopesRequest = {};
```

## Fields

| Field            | Type     | Required           | Description                            |
| ---------------- | -------- | ------------------ | -------------------------------------- |
| `organizationId` | _string_ | :heavy_minus_sign: | The organization slug to switch scopes |
| `projectId`      | _string_ | :heavy_minus_sign: | The project id to switch scopes too    |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header                         |
