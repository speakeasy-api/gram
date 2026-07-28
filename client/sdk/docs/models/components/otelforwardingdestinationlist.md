# OtelForwardingDestinationList

Wraps a list of forwarding destinations.

## Example Usage

```typescript
import { OtelForwardingDestinationList } from "@gram/client/models/components";

let value: OtelForwardingDestinationList = {
  destinations: [
    {
      createdAt: new Date("2024-01-04T17:45:11.123Z"),
      enabled: false,
      endpointUrl: "https://excitable-netsuke.net/",
      headers: [],
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      updatedAt: new Date("2025-11-20T08:03:56.425Z"),
    },
  ],
};
```

## Fields

| Field          | Type                                                                                           | Required           | Description |
| -------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `destinations` | [components.OtelForwardingDestination](../../models/components/otelforwardingdestination.md)[] | :heavy_check_mark: | N/A         |
