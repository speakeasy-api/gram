# MCPRegistry

An MCP registry

## Example Usage

```typescript
import { MCPRegistry } from "@gram/client/models/components/mcpregistry.js";

let value: MCPRegistry = {
  id: "c15e20c0-e85a-4c89-bfea-7bc71bb9d7d5",
  name: "<value>",
  url: "https://first-lotion.info",
};
```

## Fields

| Field  | Type     | Required           | Description                   |
| ------ | -------- | ------------------ | ----------------------------- |
| `id`   | _string_ | :heavy_check_mark: | Registry ID                   |
| `name` | _string_ | :heavy_check_mark: | Display name for the registry |
| `url`  | _string_ | :heavy_check_mark: | URL of the registry           |
