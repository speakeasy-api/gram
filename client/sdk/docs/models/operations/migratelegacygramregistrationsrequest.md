# MigrateLegacyGramRegistrationsRequest

## Example Usage

```typescript
import { MigrateLegacyGramRegistrationsRequest } from "@gram/client/models/operations/migratelegacygramregistrations.js";

let value: MigrateLegacyGramRegistrationsRequest = {
  migrateLegacyGramRegistrationsForm: {
    oauthProxyProviderId: "297bf755-5aa8-418e-82bb-772ad8f5d0ff",
    userSessionIssuerId: "7c2fc073-85de-41ea-9909-b39bd34077f0",
  },
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Session header                                                                                                 |
| `gramKey`                                                                                                      | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | API Key header                                                                                                 |
| `gramProject`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | project header                                                                                                 |
| `migrateLegacyGramRegistrationsForm`                                                                           | [components.MigrateLegacyGramRegistrationsForm](../../models/components/migratelegacygramregistrationsform.md) | :heavy_check_mark:                                                                                             | N/A                                                                                                            |