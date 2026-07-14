# OTELResourceAttribute

OTEL resource attribute

## Example Usage

```typescript
import { OTELResourceAttribute } from "@gram/client/models/components/otelresourceattribute.js";

let value: OTELResourceAttribute = {
  key: "<key>",
};
```

## Fields

| Field   | Type                                                                           | Required           | Description                                             |
| ------- | ------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------- |
| `key`   | _string_                                                                       | :heavy_check_mark: | Resource attribute key                                  |
| `value` | [components.OTELAttributeValue](../../models/components/otelattributevalue.md) | :heavy_minus_sign: | OTEL attribute value - any of the OTLP/JSON value kinds |
