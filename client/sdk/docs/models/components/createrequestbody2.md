# CreateRequestBody2

## Example Usage

```typescript
import { CreateRequestBody2 } from "@gram/client/models/components/createrequestbody2.js";

let value: CreateRequestBody2 = {
  mcpRegistryNamespace: "<value>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                  | Type                                                                                               | Required           | Description                                              |
| ---------------------- | -------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------- |
| `description`          | _string_                                                                                           | :heavy_minus_sign: | Description of the collection                            |
| `mcpRegistryNamespace` | _string_                                                                                           | :heavy_check_mark: | Registry namespace (e.g., 'com.speakeasy.acme.my-tools') |
| `mcpServerIds`         | _string_[]                                                                                         | :heavy_minus_sign: | MCP server IDs to attach to the collection               |
| `name`                 | _string_                                                                                           | :heavy_check_mark: | Display name for the collection                          |
| `slug`                 | _string_                                                                                           | :heavy_check_mark: | URL-friendly identifier for the collection               |
| `toolsetIds`           | _string_[]                                                                                         | :heavy_minus_sign: | Toolset IDs to attach to the collection                  |
| `visibility`           | [components.CreateRequestBody2Visibility](../../models/components/createrequestbody2visibility.md) | :heavy_minus_sign: | Visibility of the collection                             |
