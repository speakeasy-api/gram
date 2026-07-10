# RotateTunneledMcpServerKeyRequest

## Example Usage

```typescript
import { RotateTunneledMcpServerKeyRequest } from "@gram/client/models/operations/rotatetunneledmcpserverkey.js";

let value: RotateTunneledMcpServerKeyRequest = {
  rotateTunneledMcpServerKeyForm: {
    id: "a5ce529c-3207-416f-899a-b533fe8c4d11",
  },
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Session header                                                                                         |
| `gramKey`                                                                                              | *string*                                                                                               | :heavy_minus_sign:                                                                                     | API Key header                                                                                         |
| `gramProject`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | project header                                                                                         |
| `rotateTunneledMcpServerKeyForm`                                                                       | [components.RotateTunneledMcpServerKeyForm](../../models/components/rotatetunneledmcpserverkeyform.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |