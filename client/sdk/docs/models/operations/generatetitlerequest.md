# GenerateTitleRequest

## Example Usage

```typescript
import { GenerateTitleRequest } from "@gram/client/models/operations/generatetitle.js";

let value: GenerateTitleRequest = {
  generateTitleRequestBody: {
    id: "<id>",
  },
};
```

## Fields

| Field                      | Type                                                                                       | Required           | Description                |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------ | -------------------------- |
| `gramSession`              | _string_                                                                                   | :heavy_minus_sign: | Session header             |
| `gramProject`              | _string_                                                                                   | :heavy_minus_sign: | project header             |
| `gramChatSession`          | _string_                                                                                   | :heavy_minus_sign: | Chat Sessions token header |
| `generateTitleRequestBody` | [components.GenerateTitleRequestBody](../../models/components/generatetitlerequestbody.md) | :heavy_check_mark: | N/A                        |
