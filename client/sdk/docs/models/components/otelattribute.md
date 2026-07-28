# OTELAttribute

OTEL log attribute with key and typed value

## Example Usage

```typescript
import { OTELAttribute } from "@gram/client/models/components/otelattribute.js";

let value: OTELAttribute = {
  key: "<key>",
};
```

## Fields

| Field   | Type                                                                           | Required           | Description                                             |
| ------- | ------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------- |
| `key`   | _string_                                                                       | :heavy_check_mark: | Attribute key                                           |
| `value` | [components.OTELAttributeValue](../../models/components/otelattributevalue.md) | :heavy_minus_sign: | OTEL attribute value - any of the OTLP/JSON value kinds |
