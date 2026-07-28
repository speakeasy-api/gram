# DeleteOtelForwardingDestinationRequest

## Example Usage

```typescript
import { DeleteOtelForwardingDestinationRequest } from "@gram/client/models/operations";

let value: DeleteOtelForwardingDestinationRequest = {
  serveImageForm: {
    id: "<id>",
  },
};
```

## Fields

| Field            | Type                                                                   | Required           | Description    |
| ---------------- | ---------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`        | _string_                                                               | :heavy_minus_sign: | API Key header |
| `gramSession`    | _string_                                                               | :heavy_minus_sign: | Session header |
| `serveImageForm` | [components.ServeImageForm](../../models/components/serveimageform.md) | :heavy_check_mark: | N/A            |
