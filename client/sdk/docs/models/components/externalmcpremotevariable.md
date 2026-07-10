# ExternalMCPRemoteVariable

A URL template variable for a remote MCP server

## Example Usage

```typescript
import { ExternalMCPRemoteVariable } from "@gram/client/models/components/externalmcpremotevariable.js";

let value: ExternalMCPRemoteVariable = {};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `choices`                                               | *string*[]                                              | :heavy_minus_sign:                                      | Allowed values for the variable                         |
| `default`                                               | *string*                                                | :heavy_minus_sign:                                      | Default value for the variable                          |
| `description`                                           | *string*                                                | :heavy_minus_sign:                                      | Description of the variable                             |
| `isRequired`                                            | *boolean*                                               | :heavy_minus_sign:                                      | Whether this variable is required                       |
| `isSecret`                                              | *boolean*                                               | :heavy_minus_sign:                                      | Whether this variable value should be treated as secret |