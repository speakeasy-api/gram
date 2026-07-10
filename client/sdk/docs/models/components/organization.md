# Organization

## Example Usage

```typescript
import { Organization } from "@gram/client/models/components/organization.js";

let value: Organization = {
  accountType: "<value>",
  createdAt: new Date("2026-08-18T16:08:36.319Z"),
  id: "<id>",
  name: "<value>",
  slug: "<value>",
  updatedAt: new Date("2024-06-28T18:21:41.585Z"),
  webhooksEnabled: false,
  webhooksOnboarded: false,
};
```

## Fields

| Field               | Type                                                                                          | Required           | Description                                                     |
| ------------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------- |
| `accountType`       | _string_                                                                                      | :heavy_check_mark: | The account type of the organization                            |
| `createdAt`         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the organization.                          |
| `id`                | _string_                                                                                      | :heavy_check_mark: | The ID of the organization                                      |
| `name`              | _string_                                                                                      | :heavy_check_mark: | The name of the organization                                    |
| `slug`              | _string_                                                                                      | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
| `updatedAt`         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the organization.                       |
| `webhooksEnabled`   | _boolean_                                                                                     | :heavy_check_mark: | Whether webhooks are enabled for the organization               |
| `webhooksOnboarded` | _boolean_                                                                                     | :heavy_check_mark: | Whether webhooks support is initialized for the organization    |
