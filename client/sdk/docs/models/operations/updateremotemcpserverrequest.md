# UpdateRemoteMcpServerRequest

## Example Usage

```typescript
import { UpdateRemoteMcpServerRequest } from "@gram/client/models/operations/updateremotemcpserver.js";

let value: UpdateRemoteMcpServerRequest = {
  updateServerForm: {
    id: "<id>",
  },
};
```

## Fields

| Field              | Type                                                                       | Required           | Description    |
| ------------------ | -------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`      | _string_                                                                   | :heavy_minus_sign: | Session header |
| `gramKey`          | _string_                                                                   | :heavy_minus_sign: | API Key header |
| `gramProject`      | _string_                                                                   | :heavy_minus_sign: | project header |
| `updateServerForm` | [components.UpdateServerForm](../../models/components/updateserverform.md) | :heavy_check_mark: | N/A            |
