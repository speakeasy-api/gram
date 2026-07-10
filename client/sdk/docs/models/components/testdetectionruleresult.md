# TestDetectionRuleResult

## Example Usage

```typescript
import { TestDetectionRuleResult } from "@gram/client/models/components/testdetectionruleresult.js";

let value: TestDetectionRuleResult = {
  matches: [],
  supported: true,
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `matches`                                                                                | [components.TestDetectionRuleMatch](../../models/components/testdetectionrulematch.md)[] | :heavy_check_mark:                                                                       | Matches the rule found in the sample.                                                    |
| `reason`                                                                                 | *string*                                                                                 | :heavy_minus_sign:                                                                       | Why the rule isn't supported when `supported` is false.                                  |
| `supported`                                                                              | *boolean*                                                                                | :heavy_check_mark:                                                                       | False when the rule has no text-only detector (e.g. `shadow_mcp`, `destructive_tool`).   |