# HookIngestEvent

Canonical Gram feature event.

## Example Usage

```typescript
import { HookIngestEvent } from "@gram/client/models/components/hookingestevent.js";

let value: HookIngestEvent = {
  type: "assistant.thought",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `occurredAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | RFC3339 timestamp from the local agent. Defaults to receive time when absent.                 |
| `type`                                                                                        | [components.HookIngestEventType](../../models/components/hookingesteventtype.md)              | :heavy_check_mark:                                                                            | Canonical Gram hook event type.                                                               |