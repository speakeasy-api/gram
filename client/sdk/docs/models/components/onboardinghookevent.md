# OnboardingHookEvent

## Example Usage

```typescript
import { OnboardingHookEvent } from "@gram/client/models/components/onboardinghookevent.js";

let value: OnboardingHookEvent = {
  projectSlug: "<value>",
  source: "<value>",
  timeUnixNano: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                                                                               |
| -------------- | -------- | ------------------ | ----------------------------------------------------------------------------------------- |
| `chatId`       | _string_ | :heavy_minus_sign: | Gram chat/session ID that owns this event, when present.                                  |
| `eventName`    | _string_ | :heavy_minus_sign: | Hook event name (e.g. PreToolUse, SessionStart).                                          |
| `projectSlug`  | _string_ | :heavy_check_mark: | Slug of the Gram project that received the event.                                         |
| `source`       | _string_ | :heavy_check_mark: | Hook source: claude_code, cursor, or codex.                                               |
| `status`       | _string_ | :heavy_minus_sign: | Outcome status: allowed, blocked, failure, or pending.                                    |
| `timeUnixNano` | _string_ | :heavy_check_mark: | Event timestamp in nanoseconds since unix epoch. Stringified to preserve int64 precision. |
| `toolName`     | _string_ | :heavy_minus_sign: | Tool invoked by the hook, if any.                                                         |
| `userEmail`    | _string_ | :heavy_minus_sign: | Email of the user whose session produced the event, when present in hook attributes.      |
