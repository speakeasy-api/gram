# HookIngestEventType

Canonical Gram hook event type.

## Example Usage

```typescript
import { HookIngestEventType } from "@gram/client/models/components/hookingestevent.js";

let value: HookIngestEventType = "session.updated";
```

## Values

```typescript
"session.started" |
  "session.updated" |
  "session.ended" |
  "prompt.submitted" |
  "tool.requested" |
  "tool.completed" |
  "tool.failed" |
  "assistant.responded" |
  "assistant.thought" |
  "usage.reported" |
  "skill.activated" |
  "notification.reported";
```
