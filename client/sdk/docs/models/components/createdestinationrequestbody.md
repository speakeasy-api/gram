# CreateDestinationRequestBody

## Example Usage

```typescript
import { CreateDestinationRequestBody } from "@gram/client/models/components";

let value: CreateDestinationRequestBody = {
  enabled: true,
  endpointUrl: "https://insistent-lawmaker.org/",
  name: "<value>",
};
```

## Fields

| Field         | Type                                                                                           | Required           | Description                                                             |
| ------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------- |
| `enabled`     | _boolean_                                                                                      | :heavy_check_mark: | Whether the destination should be active from the moment it is created. |
| `endpointUrl` | _string_                                                                                       | :heavy_check_mark: | URL to forward OTEL payloads to.                                        |
| `headers`     | [components.OtelForwardingHeaderInput](../../models/components/otelforwardingheaderinput.md)[] | :heavy_minus_sign: | Headers to attach to every forwarded request.                           |
| `name`        | _string_                                                                                       | :heavy_check_mark: | Human-readable name. Unique within (org, project).                      |
