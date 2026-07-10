# CreateMcpEndpointRequest

## Example Usage

```typescript
import { CreateMcpEndpointRequest } from "@gram/client/models/operations/createmcpendpoint.js";

let value: CreateMcpEndpointRequest = {
  createMcpEndpointForm: {
    mcpServerId: "39db4f5a-1f85-4019-b668-3996fac16905",
    slug: "<value>",
  },
};
```

## Fields

| Field                   | Type                                                                                 | Required           | Description    |
| ----------------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`           | _string_                                                                             | :heavy_minus_sign: | Session header |
| `gramKey`               | _string_                                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`           | _string_                                                                             | :heavy_minus_sign: | project header |
| `createMcpEndpointForm` | [components.CreateMcpEndpointForm](../../models/components/createmcpendpointform.md) | :heavy_check_mark: | N/A            |
