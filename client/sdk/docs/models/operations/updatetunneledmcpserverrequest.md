# UpdateTunneledMcpServerRequest

## Example Usage

```typescript
import { UpdateTunneledMcpServerRequest } from "@gram/client/models/operations/updatetunneledmcpserver.js";

let value: UpdateTunneledMcpServerRequest = {
  updateTunneledMcpServerForm: {
    id: "c4db03b6-51cb-47eb-9c89-2e3d2784fd7c",
    name: "<value>",
  },
};
```

## Fields

| Field                         | Type                                                                                             | Required           | Description    |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`                 | _string_                                                                                         | :heavy_minus_sign: | Session header |
| `gramKey`                     | _string_                                                                                         | :heavy_minus_sign: | API Key header |
| `gramProject`                 | _string_                                                                                         | :heavy_minus_sign: | project header |
| `updateTunneledMcpServerForm` | [components.UpdateTunneledMcpServerForm](../../models/components/updatetunneledmcpserverform.md) | :heavy_check_mark: | N/A            |
