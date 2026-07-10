# GroupBy

Optional dimension to break results down by. When omitted, a single aggregate row/series for the whole slice is returned.

## Example Usage

```typescript
import { GroupBy } from "@gram/client/models/components/querypayload.js";

let value: GroupBy = "department_name";
```

## Values

```typescript
"department_name" |
  "job_title" |
  "employee_type" |
  "division_name" |
  "cost_center_name" |
  "email" |
  "model" |
  "hook_source" |
  "account_type" |
  "provider" |
  "billing_mode" |
  "query_source" |
  "skill_name" |
  "agent_name" |
  "mcp_server_name" |
  "mcp_tool_name" |
  "role" |
  "group" |
  "project_id";
```
