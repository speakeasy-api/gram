# RiskSpan

## Example Usage

```typescript
import { RiskSpan } from "@gram/client/models/components/riskspan.js";

let value: RiskSpan = {
  match: "<value>",
};
```

## Fields

| Field      | Type     | Required           | Description                                                                                                                                                                                                                         |
| ---------- | -------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `endPos`   | _number_ | :heavy_minus_sign: | End byte position within the message content.                                                                                                                                                                                       |
| `field`    | _string_ | :heavy_minus_sign: | The message field this span matched, in author-facing form (content, prompt, assistant, tool_result, or tool.name/tool.server/tool.function/tool.args). Empty for detectors that don't attribute a field (e.g. gitleaks, presidio). |
| `match`    | _string_ | :heavy_check_mark: | The matched secret or sensitive data for this span.                                                                                                                                                                                 |
| `path`     | _string_ | :heavy_minus_sign: | The JSON sub-path within the field for a `.get(...)` match (e.g. 'command', 'payload.sql'). Empty when the whole field value matched.                                                                                               |
| `startPos` | _number_ | :heavy_minus_sign: | Start byte position within the message content.                                                                                                                                                                                     |
