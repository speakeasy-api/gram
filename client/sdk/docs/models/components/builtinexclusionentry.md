# BuiltinExclusionEntry

One rule in the built-in exclusion library. Deliberately omits internal detection-engine identifiers (sources, rule ids) so they are not exposed to end users.

## Example Usage

```typescript
import { BuiltinExclusionEntry } from "@gram/client/models/components/builtinexclusionentry.js";

let value: BuiltinExclusionEntry = {
  description: "yippee although demob",
  id: "<id>",
  reason: "<value>",
};
```

## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `description`                                                           | *string*                                                                | :heavy_check_mark:                                                      | Human rationale for why these values are known-safe.                    |
| `id`                                                                    | *string*                                                                | :heavy_check_mark:                                                      | Stable rule id.                                                         |
| `reason`                                                                | *string*                                                                | :heavy_check_mark:                                                      | Label surfaced when this rule suppresses a finding.                     |
| `samples`                                                               | *string*[]                                                              | :heavy_minus_sign:                                                      | Example values — published test/documentation data, never real secrets. |