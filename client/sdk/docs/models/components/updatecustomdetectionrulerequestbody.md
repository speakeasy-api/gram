# UpdateCustomDetectionRuleRequestBody

## Example Usage

```typescript
import { UpdateCustomDetectionRuleRequestBody } from "@gram/client/models/components/updatecustomdetectionrulerequestbody.js";

let value: UpdateCustomDetectionRuleRequestBody = {
  id: "9c2f54ef-ac8a-4402-aef2-052e40abe54c",
  severity: "medium",
  title: "<value>",
};
```

## Fields

| Field           | Type                                                                                                                               | Required           | Description                                                                                              |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------- |
| `description`   | _string_                                                                                                                           | :heavy_minus_sign: | Description of what the rule detects.                                                                    |
| `detectionExpr` | _string_                                                                                                                           | :heavy_minus_sign: | CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding. |
| `id`            | _string_                                                                                                                           | :heavy_check_mark: | The custom detection rule ID.                                                                            |
| `regex`         | _string_                                                                                                                           | :heavy_minus_sign: | Deprecated legacy RE2 regex pattern; superseded by detection_expr. Accepted for backward compatibility.  |
| `severity`      | [components.UpdateCustomDetectionRuleRequestBodySeverity](../../models/components/updatecustomdetectionrulerequestbodyseverity.md) | :heavy_check_mark: | Severity level for findings produced by this rule.                                                       |
| `title`         | _string_                                                                                                                           | :heavy_check_mark: | Human-readable title for the rule.                                                                       |
