# SwitchEditingModeRequest

## Example Usage

```typescript
import { SwitchEditingModeRequest } from "@gram/client/models/operations";

let value: SwitchEditingModeRequest = {
  switchEditingModeRequestBody: {
    mode: "staging",
    slug: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `switchEditingModeRequestBody`                                                                     | [components.SwitchEditingModeRequestBody](../../models/components/switcheditingmoderequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |