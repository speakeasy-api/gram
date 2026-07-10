# QueryPayloadSortBy

Measure used to rank groups for top_n. Defaults to total_cost.

## Example Usage

```typescript
import { QueryPayloadSortBy } from "@gram/client/models/components/querypayload.js";

let value: QueryPayloadSortBy = "cache_creation_input_tokens";
```

## Values

```typescript
"total_cost" | "total_tokens" | "total_input_tokens" | "total_output_tokens" | "cache_read_input_tokens" | "cache_creation_input_tokens" | "total_tool_calls" | "total_chats"
```