# GetRemoteMcpServerRequest

## Example Usage

```typescript
import { GetRemoteMcpServerRequest } from "@gram/client/models/operations/getremotemcpserver.js";

let value: GetRemoteMcpServerRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                                    |
| ------------- | -------- | ------------------ | -------------------------------------------------------------- |
| `id`          | _string_ | :heavy_minus_sign: | The ID of the remote MCP server. Mutually exclusive with slug. |
| `slug`        | _string_ | :heavy_minus_sign: | The slug of the remote MCP server. Mutually exclusive with id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                 |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                 |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                                 |
