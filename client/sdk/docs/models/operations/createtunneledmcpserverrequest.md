# CreateTunneledMcpServerRequest

## Example Usage

```typescript
import { CreateTunneledMcpServerRequest } from "@gram/client/models/operations/createtunneledmcpserver.js";

let value: CreateTunneledMcpServerRequest = {
  createTunneledMcpServerForm: {
    name: "<value>",
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |
| `createTunneledMcpServerForm`                                                                    | [components.CreateTunneledMcpServerForm](../../models/components/createtunneledmcpserverform.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |