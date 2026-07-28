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

| Field         | Type       | Required           | Description                                                                                                   |
| ------------- | ---------- | ------------------ | ------------------------------------------------------------------------------------------------------------- |
| `confidence`  | _number_   | :heavy_check_mark: | Confidence score in the range 0.0 to 1.0.                                                                     |
| `description` | _string_   | :heavy_minus_sign: | Human-readable description of why this match was flagged.                                                     |
| `endPos`      | _number_   | :heavy_check_mark: | Exclusive end byte offset of the match in the sample.                                                         |
| `match`       | _string_   | :heavy_check_mark: | Matched substring of the sample.                                                                              |
| `ruleId`      | _string_   | :heavy_check_mark: | Canonical rule id of the match (may differ from the requested rule id when one input matches multiple rules). |
| `source`      | _string_   | :heavy_check_mark: | Detection source (e.g. `gitleaks`, `presidio`, `prompt_injection`, `custom`).                                 |
| `startPos`    | _number_   | :heavy_check_mark: | Inclusive start byte offset of the match in the sample.                                                       |
| `tags`        | _string_[] | :heavy_minus_sign: | Tags from the underlying rule.                                                                                |
