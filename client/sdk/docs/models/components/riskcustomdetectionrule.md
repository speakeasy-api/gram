# RiskCustomDetectionRule

## Example Usage

```typescript
import { RiskCustomDetectionRule } from "@gram/client/models/components/riskcustomdetectionrule.js";

let value: RiskCustomDetectionRule = {
  createdAt: new Date("2025-07-30T06:29:31.408Z"),
  description: "wide-eyed within minus t-shirt underneath boastfully beneath",
  id: "ac72204d-48b3-428b-9dd3-4f8dff93a1c7",
  regex: "<value>",
  ruleId: "<id>",
  severity: "info",
  title: "<value>",
  updatedAt: new Date("2026-01-25T04:56:04.150Z"),
};
```

## Fields

| Field           | Type                                                                                                     | Required           | Description                                                                                                                                                                        |
| --------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `createdAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)            | :heavy_check_mark: | When the custom detection rule was created.                                                                                                                                        |
| `description`   | _string_                                                                                                 | :heavy_check_mark: | Description of what the rule detects.                                                                                                                                              |
| `detectionExpr` | _string_                                                                                                 | :heavy_minus_sign: | CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding. Supersedes regex.                                                         |
| `id`            | _string_                                                                                                 | :heavy_check_mark: | The custom detection rule ID.                                                                                                                                                      |
| `regex`         | _string_                                                                                                 | :heavy_check_mark: | Legacy RE2-compatible regex pattern (read-only). Live for existing rules; evaluated as content.match(regex) when detection_expr is empty. New rules author detection_expr instead. |
| `ruleId`        | _string_                                                                                                 | :heavy_check_mark: | Stable rule identifier, prefixed with `custom.`.                                                                                                                                   |
| `severity`      | [components.RiskCustomDetectionRuleSeverity](../../models/components/riskcustomdetectionruleseverity.md) | :heavy_check_mark: | Severity level for findings produced by this rule.                                                                                                                                 |
| `title`         | _string_                                                                                                 | :heavy_check_mark: | Human-readable title for the rule.                                                                                                                                                 |
| `updatedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)            | :heavy_check_mark: | When the custom detection rule was last updated.                                                                                                                                   |
