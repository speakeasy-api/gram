# OTELResource

OTEL resource information

## Example Usage

```typescript
import { OTELResource } from "@gram/client/models/components/otelresource.js";

let value: OTELResource = {};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `attributes`                                                                           | [components.OTELResourceAttribute](../../models/components/otelresourceattribute.md)[] | :heavy_minus_sign:                                                                     | Resource attributes                                                                    |
| `droppedAttributesCount`                                                               | *number*                                                                               | :heavy_minus_sign:                                                                     | Number of dropped attributes                                                           |