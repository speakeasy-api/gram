# CreateEnvironmentRequest

## Example Usage

```typescript
import { CreateEnvironmentRequest } from "@gram/client/models/operations/createenvironment.js";

let value: CreateEnvironmentRequest = {
  createEnvironmentForm: {
    entries: [
      {
        name: "<value>",
        value: "<value>",
      },
    ],
    name: "<value>",
    organizationId: "<id>",
  },
};
```

## Fields

| Field                   | Type                                                                                 | Required           | Description    |
| ----------------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`           | _string_                                                                             | :heavy_minus_sign: | Session header |
| `gramProject`           | _string_                                                                             | :heavy_minus_sign: | project header |
| `createEnvironmentForm` | [components.CreateEnvironmentForm](../../models/components/createenvironmentform.md) | :heavy_check_mark: | N/A            |
