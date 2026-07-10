# SetToolsetToolVariationsGroupRequest

## Example Usage

```typescript
import { SetToolsetToolVariationsGroupRequest } from "@gram/client/models/operations/settoolsettoolvariationsgroup.js";

let value: SetToolsetToolVariationsGroupRequest = {
  slug: "<value>",
  setToolVariationsGroupRequestBody: {},
};
```

## Fields

| Field                               | Type                                                                                                         | Required           | Description                          |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------ |
| `slug`                              | _string_                                                                                                     | :heavy_check_mark: | The slug of the toolset to configure |
| `gramSession`                       | _string_                                                                                                     | :heavy_minus_sign: | Session header                       |
| `gramKey`                           | _string_                                                                                                     | :heavy_minus_sign: | API Key header                       |
| `gramProject`                       | _string_                                                                                                     | :heavy_minus_sign: | project header                       |
| `setToolVariationsGroupRequestBody` | [components.SetToolVariationsGroupRequestBody](../../models/components/settoolvariationsgrouprequestbody.md) | :heavy_check_mark: | N/A                                  |
