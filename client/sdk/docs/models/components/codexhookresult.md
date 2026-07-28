# CodexHookResult

Result for Codex hook events

## Example Usage

```typescript
import { CodexHookResult } from "@gram/client/models/components/codexhookresult.js";

let value: CodexHookResult = {};
```

## Fields

| Field      | Type     | Required           | Description                                            |
| ---------- | -------- | ------------------ | ------------------------------------------------------ |
| `decision` | _string_ | :heavy_minus_sign: | Permission decision for blocking events: allow or deny |
| `reason`   | _string_ | :heavy_minus_sign: | Reason for the decision, shown to the user             |
