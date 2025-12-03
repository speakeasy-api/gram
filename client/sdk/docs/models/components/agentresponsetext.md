# AgentResponseText

Text format configuration for the response

## Example Usage

```typescript
import { AgentResponseText } from "@gram/client/models/components";

let value: AgentResponseText = {
  format: {
    type: "<value>",
  },
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `format`                                                                 | [components.AgentTextFormat](../../models/components/agenttextformat.md) | :heavy_check_mark:                                                       | Text format type                                                         |