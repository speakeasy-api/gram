# CheckMCPSlugAvailabilityRequest

## Example Usage

```typescript
import { CheckMCPSlugAvailabilityRequest } from "@gram/client/models/operations";

let value: CheckMCPSlugAvailabilityRequest = {
  slug: "<value>",
};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `slug`             | *string*           | :heavy_check_mark: | The slug to check  |
| `gramSession`      | *string*           | :heavy_minus_sign: | Session header     |
| `gramKey`          | *string*           | :heavy_minus_sign: | API Key header     |
| `gramProject`      | *string*           | :heavy_minus_sign: | project header     |