# CanonicalToolAttributes

The original details of a tool

## Example Usage

```typescript
import { CanonicalToolAttributes } from "@gram/client/models/components";

let value: CanonicalToolAttributes = {
  description: "almost inasmuch beside disloyal painfully",
  name: "<value>",
  variationId: "<id>",
};
```

## Fields

| Field                                                | Type                                                 | Required                                             | Description                                          |
| ---------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------- |
| `confirm`                                            | *string*                                             | :heavy_minus_sign:                                   | Confirmation mode for the tool                       |
| `confirmPrompt`                                      | *string*                                             | :heavy_minus_sign:                                   | Prompt for the confirmation                          |
| `description`                                        | *string*                                             | :heavy_check_mark:                                   | Description of the tool                              |
| `name`                                               | *string*                                             | :heavy_check_mark:                                   | The name of the tool                                 |
| `summarizer`                                         | *string*                                             | :heavy_minus_sign:                                   | Summarizer for the tool                              |
| `variationId`                                        | *string*                                             | :heavy_check_mark:                                   | The ID of the variation that was applied to the tool |