# SortBy

Measure used to rank sessions. Defaults to total_cost.

## Example Usage

```typescript
import { SortBy } from "@gram/client/models/components/listsessionspayload.js";

let value: SortBy = "duration_seconds";
```

## Values

```typescript
"total_cost" |
  "total_tokens" |
  "total_input_tokens" |
  "total_output_tokens" |
  "tool_call_count" |
  "message_count" |
  "duration_seconds";
```
