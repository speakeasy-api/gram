# CreateCustomDetectionRuleRequestBody

## Example Usage

```typescript
import { CreateCustomDetectionRuleRequestBody } from "@gram/client/models/components/createcustomdetectionrulerequestbody.js";

let value: CreateCustomDetectionRuleRequestBody = {
  ruleId: "<id>",
  title: "<value>",
};
```

## Fields

| Field           | Type                                                       | Required           | Description                                                                                              |
| --------------- | ---------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------- |
| `description`   | _string_                                                   | :heavy_minus_sign: | Description of what the rule detects.                                                                    |
| `detectionExpr` | _string_                                                   | :heavy_minus_sign: | CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding. |
| `regex`         | _string_                                                   | :heavy_minus_sign: | Deprecated legacy RE2 regex pattern; superseded by detection_expr. Accepted for backward compatibility.  |
| `ruleId`        | _string_                                                   | :heavy_check_mark: | Stable rule identifier, prefixed with `custom.`.                                                         |
| `severity`      | [components.Severity](../../models/components/severity.md) | :heavy_minus_sign: | Severity level for findings produced by this rule.                                                       |
| `title`         | _string_                                                   | :heavy_check_mark: | Human-readable title for the rule.                                                                       |
