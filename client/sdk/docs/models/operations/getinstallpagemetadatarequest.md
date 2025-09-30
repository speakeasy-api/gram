# GetInstallPageMetadataRequest

## Example Usage

```typescript
import { GetInstallPageMetadataRequest } from "@gram/client/models/operations";

let value: GetInstallPageMetadataRequest = {
  toolsetId: "3092a9b2-7efb-4ba3-9d85-72eb6a4e9310",
};
```

## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `toolsetId`                                            | *string*                                               | :heavy_check_mark:                                     | The toolset associated with this install page metadata |
| `gramSession`                                          | *string*                                               | :heavy_minus_sign:                                     | Session header                                         |
| `gramProject`                                          | *string*                                               | :heavy_minus_sign:                                     | project header                                         |