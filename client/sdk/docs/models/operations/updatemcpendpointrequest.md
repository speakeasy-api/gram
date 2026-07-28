# UpdateMcpEndpointRequest

## Example Usage

```typescript
import { UpdateMcpEndpointRequest } from "@gram/client/models/operations/updatemcpendpoint.js";

let value: UpdateMcpEndpointRequest = {
  updateMcpEndpointForm: {
    id: "b6b92503-33eb-4f10-965c-0414ee035281",
    mcpServerId: "ba0f62e4-a06a-465d-b316-1741cc0a81dc",
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
| `updateMcpEndpointForm` | [components.UpdateMcpEndpointForm](../../models/components/updatemcpendpointform.md) | :heavy_check_mark: | N/A            |
