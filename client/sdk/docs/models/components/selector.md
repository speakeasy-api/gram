# Selector

A constraint that narrows which resources a grant applies to.

## Example Usage

```typescript
import { Selector } from "@gram/client/models/components/selector.js";

let value: Selector = {
  resourceId: "<id>",
  resourceKind: "environment",
};
```

## Fields

| Field          | Type                                                                             | Required           | Description                                                                                                    |
| -------------- | -------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------- |
| `disposition`  | [components.SelectorDisposition](../../models/components/selectordisposition.md) | :heavy_minus_sign: | Tool disposition filter (MCP scopes only).                                                                     |
| `projectId`    | _string_                                                                         | :heavy_minus_sign: | Project filter (MCP scopes only). When set with resource_id='\*', grants access to all servers in the project. |
| `resourceId`   | _string_                                                                         | :heavy_check_mark: | The resource identifier, or '\*' for all resources of this kind.                                               |
| `resourceKind` | [components.ResourceKind](../../models/components/resourcekind.md)               | :heavy_check_mark: | The kind of resource this selector targets.                                                                    |
| `serverUrl`    | _string_                                                                         | :heavy_minus_sign: | Server URL filter (risk policy scopes only). Include the URI scheme, for example https://api.example.com.      |
| `tool`         | _string_                                                                         | :heavy_minus_sign: | Specific tool name filter (MCP scopes only).                                                                   |
