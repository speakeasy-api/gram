# OrganizationEntry

## Example Usage

```typescript
import { OrganizationEntry } from "@gram/client/models/components/organizationentry.js";

let value: OrganizationEntry = {
  id: "<id>",
  name: "<value>",
  projects: [
    {
      id: "<id>",
      name: "<value>",
      slug: "<value>",
    },
  ],
  slug: "<value>",
};
```

## Fields

| Field                | Type                                                                 | Required           | Description                                                                       |
| -------------------- | -------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------- |
| `id`                 | _string_                                                             | :heavy_check_mark: | N/A                                                                               |
| `name`               | _string_                                                             | :heavy_check_mark: | N/A                                                                               |
| `projects`           | [components.ProjectEntry](../../models/components/projectentry.md)[] | :heavy_check_mark: | N/A                                                                               |
| `scimEnabled`        | _boolean_                                                            | :heavy_minus_sign: | Whether SCIM directory sync is enabled for this organization (synced from WorkOS) |
| `slug`               | _string_                                                             | :heavy_check_mark: | N/A                                                                               |
| `ssoEnabled`         | _boolean_                                                            | :heavy_minus_sign: | Whether SSO is enabled for this organization (synced from WorkOS)                 |
| `userWorkspaceSlugs` | _string_[]                                                           | :heavy_minus_sign: | N/A                                                                               |
