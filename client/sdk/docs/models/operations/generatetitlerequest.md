# GenerateTitleRequest

## Example Usage

```typescript
import { GenerateTitleRequest } from "@gram/client/models/operations";

let value: GenerateTitleRequest = {
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