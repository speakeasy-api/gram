# ExternalMCPRemoteVariable

A URL template variable for a remote MCP server

## Example Usage

```typescript
import { ExternalMCPRemoteVariable } from "@gram/client/models/components/externalmcpremotevariable.js";

let value: ExternalMCPRemoteVariable = {};
```

## Fields

| Field         | Type       | Required           | Description                                             |
| ------------- | ---------- | ------------------ | ------------------------------------------------------- |
| `choices`     | _string_[] | :heavy_minus_sign: | Allowed values for the variable                         |
| `default`     | _string_   | :heavy_minus_sign: | Default value for the variable                          |
| `description` | _string_   | :heavy_minus_sign: | Description of the variable                             |
| `isRequired`  | _boolean_  | :heavy_minus_sign: | Whether this variable is required                       |
| `isSecret`    | _boolean_  | :heavy_minus_sign: | Whether this variable value should be treated as secret |
