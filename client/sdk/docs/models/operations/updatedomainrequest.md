# UpdateDomainRequest

## Example Usage

```typescript
import { UpdateDomainRequest } from "@gram/client/models/operations/updatedomain.js";

let value: UpdateDomainRequest = {
  updateDomainRequestBody: {
    ipAllowlist: [],
  },
};
```

## Fields

| Field                     | Type                                                                                     | Required           | Description    |
| ------------------------- | ---------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`             | _string_                                                                                 | :heavy_minus_sign: | Session header |
| `updateDomainRequestBody` | [components.UpdateDomainRequestBody](../../models/components/updatedomainrequestbody.md) | :heavy_check_mark: | N/A            |
