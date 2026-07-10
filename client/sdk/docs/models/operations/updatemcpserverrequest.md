# UpdateMcpServerRequest

## Example Usage

```typescript
import { UpdateMcpServerRequest } from "@gram/client/models/operations/updatemcpserver.js";

let value: UpdateMcpServerRequest = {
  updateMcpServerForm: {
    id: "d0f4219c-43b4-47f9-abc7-bef89cc8807c",
    visibility: "private",
  },
};
```

## Fields

| Field                 | Type                                                                             | Required           | Description    |
| --------------------- | -------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`         | _string_                                                                         | :heavy_minus_sign: | Session header |
| `gramKey`             | _string_                                                                         | :heavy_minus_sign: | API Key header |
| `gramProject`         | _string_                                                                         | :heavy_minus_sign: | project header |
| `updateMcpServerForm` | [components.UpdateMcpServerForm](../../models/components/updatemcpserverform.md) | :heavy_check_mark: | N/A            |
