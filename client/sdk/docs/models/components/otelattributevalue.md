# OTELAttributeValue

OTEL attribute value - any of the OTLP/JSON value kinds

## Example Usage

```typescript
import { OTELAttributeValue } from "@gram/client/models/components/otelattributevalue.js";

let value: OTELAttributeValue = {};
```

## Fields

| Field         | Type      | Required           | Description                                                 |
| ------------- | --------- | ------------------ | ----------------------------------------------------------- |
| `arrayValue`  | _any_     | :heavy_minus_sign: | Array value (passed through)                                |
| `boolValue`   | _boolean_ | :heavy_minus_sign: | Boolean value                                               |
| `bytesValue`  | _string_  | :heavy_minus_sign: | Bytes value (base64-encoded per OTLP/JSON)                  |
| `doubleValue` | _number_  | :heavy_minus_sign: | Double value                                                |
| `intValue`    | _any_     | :heavy_minus_sign: | Integer value (string-encoded per OTLP/JSON, or raw number) |
| `kvlistValue` | _any_     | :heavy_minus_sign: | Key-value list value (passed through)                       |
| `stringValue` | _string_  | :heavy_minus_sign: | String value                                                |
