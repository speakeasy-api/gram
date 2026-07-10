# CursorHookResult

Result for Cursor hook events

## Example Usage

```typescript
import { CursorHookResult } from "@gram/client/models/components/cursorhookresult.js";

let value: CursorHookResult = {};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `additionalContext`                                                          | *string*                                                                     | :heavy_minus_sign:                                                           | Additional context to inject into the conversation                           |
| `agentMessage`                                                               | *string*                                                                     | :heavy_minus_sign:                                                           | Message sent back to the agent (beforeMCPExecution only)                     |
| `permission`                                                                 | *string*                                                                     | :heavy_minus_sign:                                                           | Permission decision for preToolUse / beforeMCPExecution: allow, deny, or ask |
| `userMessage`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Message to display to the user                                               |