# CanonicalToolAttributes

The original details of a tool

## Example Usage

```typescript
import { CanonicalToolAttributes } from "@gram/client/models/components/canonicaltoolattributes.js";

let value: CanonicalToolAttributes = {
  description: "almost inasmuch beside disloyal painfully",
  name: "<value>",
  variationId: "<id>",
};
```

## Fields

| Field           | Type     | Required           | Description                                          |
| --------------- | -------- | ------------------ | ---------------------------------------------------- |
| `confirm`       | _string_ | :heavy_minus_sign: | Confirmation mode for the tool                       |
| `confirmPrompt` | _string_ | :heavy_minus_sign: | Prompt for the confirmation                          |
| `description`   | _string_ | :heavy_check_mark: | Description of the tool                              |
| `name`          | _string_ | :heavy_check_mark: | The name of the tool                                 |
| `summarizer`    | _string_ | :heavy_minus_sign: | Summarizer for the tool                              |
| `variationId`   | _string_ | :heavy_check_mark: | The ID of the variation that was applied to the tool |
