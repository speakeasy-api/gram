# UpsertGlobalVariationRequest

## Example Usage

```typescript
import { UpsertGlobalVariationRequest } from "@gram/client/models/operations";

let value: UpsertGlobalVariationRequest = {
  upsertGlobalToolVariationForm: {
    srcToolName: "<value>",
    srcToolUrn: "<value>",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |
| `upsertGlobalToolVariationForm`                                                                      | [components.UpsertGlobalToolVariationForm](../../models/components/upsertglobaltoolvariationform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |