# UpdateToolsetRequest

## Example Usage

```typescript
import { UpdateToolsetRequest } from "@gram/client/models/operations/updatetoolset.js";

let value: UpdateToolsetRequest = {
  slug: "<value>",
  updateToolsetRequestBody: {},
};
```

## Fields

| Field                      | Type                                                                                       | Required           | Description                       |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------ | --------------------------------- |
| `slug`                     | _string_                                                                                   | :heavy_check_mark: | The slug of the toolset to update |
| `gramSession`              | _string_                                                                                   | :heavy_minus_sign: | Session header                    |
| `gramKey`                  | _string_                                                                                   | :heavy_minus_sign: | API Key header                    |
| `gramProject`              | _string_                                                                                   | :heavy_minus_sign: | project header                    |
| `updateToolsetRequestBody` | [components.UpdateToolsetRequestBody](../../models/components/updatetoolsetrequestbody.md) | :heavy_check_mark: | N/A                               |
