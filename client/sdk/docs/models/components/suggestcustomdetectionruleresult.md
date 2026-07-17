# SuggestCustomDetectionRuleResult

## Example Usage

```typescript
import { SuggestCustomDetectionRuleResult } from "@gram/client/models/components/suggestcustomdetectionruleresult.js";

let value: SuggestCustomDetectionRuleResult = {
  description: "trick wherever when evenly eek",
  regex: "<value>",
  ruleId: "<id>",
  severity: "high",
  title: "<value>",
};
```

## Fields

| Field           | Type                                                                                                                       | Required           | Description                                                                                           |
| --------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------- |
| `description`   | _string_                                                                                                                   | :heavy_check_mark: | Description of what the rule detects and why it matters.                                              |
| `detectionExpr` | _string_                                                                                                                   | :heavy_minus_sign: | Suggested CEL detection predicate.                                                                    |
| `regex`         | _string_                                                                                                                   | :heavy_check_mark: | Deprecated legacy regex suggestion; superseded by detection_expr. Present for backward compatibility. |
| `ruleId`        | _string_                                                                                                                   | :heavy_check_mark: | Suggested stable identifier, prefixed with `custom.`.                                                 |
| `severity`      | [components.SuggestCustomDetectionRuleResultSeverity](../../models/components/suggestcustomdetectionruleresultseverity.md) | :heavy_check_mark: | Suggested severity level.                                                                             |
| `title`         | _string_                                                                                                                   | :heavy_check_mark: | Short, human-friendly title for the rule.                                                             |
