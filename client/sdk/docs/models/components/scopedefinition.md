# ScopeDefinition

## Example Usage

```typescript
import { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";

let value: ScopeDefinition = {
  description: "story until guilty",
  resourceType: "environment",
  slug: "mcp:blocked_connect",
  visibility: "user_visible",
};
```

## Fields

| Field                                                                                   | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `description`                                                                           | *string*                                                                                | :heavy_check_mark:                                                                      | What this scope protects.                                                               |
| `exclusionScope`                                                                        | [components.ExclusionScope](../../models/components/exclusionscope.md)                  | :heavy_minus_sign:                                                                      | The scope used to store exception rules for this scope.                                 |
| `resourceType`                                                                          | [components.ResourceType](../../models/components/resourcetype.md)                      | :heavy_check_mark:                                                                      | The type of resource this scope applies to.                                             |
| `slug`                                                                                  | [components.Slug](../../models/components/slug.md)                                      | :heavy_check_mark:                                                                      | Unique scope identifier.                                                                |
| `visibility`                                                                            | [components.Visibility](../../models/components/visibility.md)                          | :heavy_check_mark:                                                                      | Whether this scope is a first-class permission or an internal storage/evaluation scope. |