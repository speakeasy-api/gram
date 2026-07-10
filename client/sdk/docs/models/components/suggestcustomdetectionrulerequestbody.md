# SuggestCustomDetectionRuleRequestBody

## Example Usage

```typescript
import { SuggestCustomDetectionRuleRequestBody } from "@gram/client/models/components/suggestcustomdetectionrulerequestbody.js";

let value: SuggestCustomDetectionRuleRequestBody = {
  prompt: "<value>",
};
```

## Fields

| Field             | Type       | Required           | Description                                                                       |
| ----------------- | ---------- | ------------------ | --------------------------------------------------------------------------------- |
| `existingRuleIds` | _string_[] | :heavy_minus_sign: | Existing built-in and custom rule ids the suggested id must avoid colliding with. |
| `prompt`          | _string_   | :heavy_check_mark: | Natural-language description of what the rule should detect.                      |
