# CreateMcpServerRequest

## Example Usage

```typescript
import { CreateMcpServerRequest } from "@gram/client/models/operations/createmcpserver.js";

let value: CreateMcpServerRequest = {
  createMcpServerForm: {
    name: "<value>",
    visibility: "disabled",
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `gramKey`                                                                        | *string*                                                                         | :heavy_minus_sign:                                                               | API Key header                                                                   |
| `gramProject`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | project header                                                                   |
| `createMcpServerForm`                                                            | [components.CreateMcpServerForm](../../models/components/createmcpserverform.md) | :heavy_check_mark:                                                               | N/A                                                                              |