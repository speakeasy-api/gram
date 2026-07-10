# AssistantToolsetRef

## Example Usage

```typescript
import { AssistantToolsetRef } from "@gram/client/models/components/assistanttoolsetref.js";

let value: AssistantToolsetRef = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                     | Type                                                      | Required                                                  | Description                                               |
| --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- |
| `environmentSlug`                                         | *string*                                                  | :heavy_minus_sign:                                        | Optional environment slug used when invoking the toolset. |
| `toolsetSlug`                                             | *string*                                                  | :heavy_check_mark:                                        | The toolset slug exposed to the assistant.                |