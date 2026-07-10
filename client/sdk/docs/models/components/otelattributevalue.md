# OTELAttributeValue

OTEL attribute value - any of the OTLP/JSON value kinds

## Example Usage

```typescript
import { OTELAttributeValue } from "@gram/client/models/components/otelattributevalue.js";

let value: OTELAttributeValue = {};
```

## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `arrayValue`                                                | *any*                                                       | :heavy_minus_sign:                                          | Array value (passed through)                                |
| `boolValue`                                                 | *boolean*                                                   | :heavy_minus_sign:                                          | Boolean value                                               |
| `bytesValue`                                                | *string*                                                    | :heavy_minus_sign:                                          | Bytes value (base64-encoded per OTLP/JSON)                  |
| `doubleValue`                                               | *number*                                                    | :heavy_minus_sign:                                          | Double value                                                |
| `intValue`                                                  | *any*                                                       | :heavy_minus_sign:                                          | Integer value (string-encoded per OTLP/JSON, or raw number) |
| `kvlistValue`                                               | *any*                                                       | :heavy_minus_sign:                                          | Key-value list value (passed through)                       |
| `stringValue`                                               | *string*                                                    | :heavy_minus_sign:                                          | String value                                                |