# UpsertGlobalVariationRequest

## Example Usage

```typescript
import { UpsertGlobalVariationRequest } from "@gram/client/models/operations/upsertglobalvariation.js";

let value: UpsertGlobalVariationRequest = {
  upsertGlobalToolVariationForm: {
    srcToolName: "<value>",
    srcToolUrn: "<value>",
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description    |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header |
| `gramKey`                       | _string_                                                                                             | :heavy_minus_sign: | API Key header |
| `gramProject`                   | _string_                                                                                             | :heavy_minus_sign: | project header |
| `upsertGlobalToolVariationForm` | [components.UpsertGlobalToolVariationForm](../../models/components/upsertglobaltoolvariationform.md) | :heavy_check_mark: | N/A            |
