# UpdateToolsetRequest

## Example Usage

```typescript
import { UpdateToolsetRequest } from "@gram/client/models/operations";

let value: UpdateToolsetRequest = {
  slug: "<value>",
  updateToolsetRequestBody: {},
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `slug`                                                                                     | *string*                                                                                   | :heavy_check_mark:                                                                         | The slug of the toolset to update                                                          |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramKey`                                                                                  | *string*                                                                                   | :heavy_minus_sign:                                                                         | API Key header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `updateToolsetRequestBody`                                                                 | [components.UpdateToolsetRequestBody](../../models/components/updatetoolsetrequestbody.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |