# GetTitleRequest

## Example Usage

```typescript
import { GetTitleRequest } from "@gram/client/models/operations";

let value: GetTitleRequest = {
  serveImageForm: {
    id: "<id>",
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `gramSession`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Session header                                                         |
| `gramProject`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | project header                                                         |
| `gramChatSession`                                                      | *string*                                                               | :heavy_minus_sign:                                                     | Chat Sessions token header                                             |
| `serveImageForm`                                                       | [components.ServeImageForm](../../models/components/serveimageform.md) | :heavy_check_mark:                                                     | N/A                                                                    |