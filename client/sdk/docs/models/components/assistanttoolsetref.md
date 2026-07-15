# AssistantToolsetRef

## Example Usage

```typescript
import { AssistantToolsetRef } from "@gram/client/models/components/assistanttoolsetref.js";

let value: AssistantToolsetRef = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field             | Type     | Required           | Description                                               |
| ----------------- | -------- | ------------------ | --------------------------------------------------------- |
| `environmentSlug` | _string_ | :heavy_minus_sign: | Optional environment slug used when invoking the toolset. |
| `toolsetSlug`     | _string_ | :heavy_check_mark: | The toolset slug exposed to the assistant.                |
