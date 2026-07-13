# GetMcpEndpointRequest

## Example Usage

```typescript
import { GetMcpEndpointRequest } from "@gram/client/models/operations/getmcpendpoint.js";

let value: GetMcpEndpointRequest = {};
```

## Fields

| Field            | Type     | Required           | Description                                                                                                    |
| ---------------- | -------- | ------------------ | -------------------------------------------------------------------------------------------------------------- |
| `id`             | _string_ | :heavy_minus_sign: | The ID of the MCP endpoint                                                                                     |
| `customDomainId` | _string_ | :heavy_minus_sign: | The ID of the custom domain the endpoint slug is registered under. Omit to look up a platform-domain endpoint. |
| `slug`           | _string_ | :heavy_minus_sign: | The slug to look up                                                                                            |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header                                                                                                 |
| `gramKey`        | _string_ | :heavy_minus_sign: | API Key header                                                                                                 |
| `gramProject`    | _string_ | :heavy_minus_sign: | project header                                                                                                 |
