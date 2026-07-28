# RiskExclusionMatchType

How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>').

## Example Usage

```typescript
import { RiskExclusionMatchType } from "@gram/client/models/components/riskexclusion.js";

let value: RiskExclusionMatchType = "entity_type";
```

## Values

```typescript
"exact" | "regex" | "rule_id" | "source" | "entity_type";
```
