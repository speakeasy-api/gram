# PolicyType

Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge). Defaults to standard.

## Example Usage

```typescript
import { PolicyType } from "@gram/client/models/components/createriskpolicyrequestbody.js";

let value: PolicyType = "prompt_based";
```

## Values

```typescript
"standard" | "prompt_based"
```