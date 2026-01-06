# SetIterationModeRequest

## Example Usage

```typescript
import { SetIterationModeRequest } from "@gram/client/models/operations";

let value: SetIterationModeRequest = {
  slug: "<value>",
  setIterationModeRequestBody: {
    iterationMode: true,
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `slug`                                                                                           | *string*                                                                                         | :heavy_check_mark:                                                                               | The slug of the toolset                                                                          |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |
| `setIterationModeRequestBody`                                                                    | [components.SetIterationModeRequestBody](../../models/components/setiterationmoderequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |