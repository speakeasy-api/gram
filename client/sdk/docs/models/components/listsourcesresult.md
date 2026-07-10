# ListSourcesResult

## Example Usage

```typescript
import { ListSourcesResult } from "@gram/client/models/components/listsourcesresult.js";

let value: ListSourcesResult = {
  sources: ["<value 1>"],
};
```

## Fields

| Field     | Type       | Required           | Description                                                                                                                   |
| --------- | ---------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| `sources` | _string_[] | :heavy_check_mark: | The distinct agent sources present in this project's chats (raw source strings such as 'claude-code', 'Codex', 'playground'). |
