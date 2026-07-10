# TestDetectionRuleMatch

## Example Usage

```typescript
import { TestDetectionRuleMatch } from "@gram/client/models/components/testdetectionrulematch.js";

let value: TestDetectionRuleMatch = {
  confidence: 2153.74,
  endPos: 978893,
  match: "<value>",
  ruleId: "<id>",
  source: "<value>",
  startPos: 367558,
};
```

## Fields

| Field                                                                                                         | Type                                                                                                          | Required                                                                                                      | Description                                                                                                   |
| ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `confidence`                                                                                                  | *number*                                                                                                      | :heavy_check_mark:                                                                                            | Confidence score in the range 0.0 to 1.0.                                                                     |
| `description`                                                                                                 | *string*                                                                                                      | :heavy_minus_sign:                                                                                            | Human-readable description of why this match was flagged.                                                     |
| `endPos`                                                                                                      | *number*                                                                                                      | :heavy_check_mark:                                                                                            | Exclusive end byte offset of the match in the sample.                                                         |
| `match`                                                                                                       | *string*                                                                                                      | :heavy_check_mark:                                                                                            | Matched substring of the sample.                                                                              |
| `ruleId`                                                                                                      | *string*                                                                                                      | :heavy_check_mark:                                                                                            | Canonical rule id of the match (may differ from the requested rule id when one input matches multiple rules). |
| `source`                                                                                                      | *string*                                                                                                      | :heavy_check_mark:                                                                                            | Detection source (e.g. `gitleaks`, `presidio`, `prompt_injection`, `custom`).                                 |
| `startPos`                                                                                                    | *number*                                                                                                      | :heavy_check_mark:                                                                                            | Inclusive start byte offset of the match in the sample.                                                       |
| `tags`                                                                                                        | *string*[]                                                                                                    | :heavy_minus_sign:                                                                                            | Tags from the underlying rule.                                                                                |