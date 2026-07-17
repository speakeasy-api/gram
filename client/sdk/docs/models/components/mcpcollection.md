# MCPCollection

An MCP collection within an organization

## Example Usage

```typescript
import { MCPCollection } from "@gram/client/models/components/mcpcollection.js";

let value: MCPCollection = {
  id: "bf41bafd-a0cb-4a6a-9d62-edbccf74eef7",
  name: "<value>",
  slug: "<value>",
  visibility: "private",
};
```

## Fields

| Field                  | Type                                                                                     | Required           | Description                     |
| ---------------------- | ---------------------------------------------------------------------------------------- | ------------------ | ------------------------------- |
| `description`          | _string_                                                                                 | :heavy_minus_sign: | Description of the collection   |
| `id`                   | _string_                                                                                 | :heavy_check_mark: | Collection ID                   |
| `mcpRegistryNamespace` | _string_                                                                                 | :heavy_minus_sign: | Registry namespace              |
| `name`                 | _string_                                                                                 | :heavy_check_mark: | Display name for the collection |
| `slug`                 | _string_                                                                                 | :heavy_check_mark: | URL-friendly identifier         |
| `visibility`           | [components.MCPCollectionVisibility](../../models/components/mcpcollectionvisibility.md) | :heavy_check_mark: | Visibility of the collection    |
