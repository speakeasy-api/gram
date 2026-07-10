# MigrateLegacyGramRegistrationsForm

Form for migrating legacy gram OAuth-proxy client registrations onto a user_session_issuer.

## Example Usage

```typescript
import { MigrateLegacyGramRegistrationsForm } from "@gram/client/models/components/migratelegacygramregistrationsform.js";

let value: MigrateLegacyGramRegistrationsForm = {
  oauthProxyProviderId: "6b24289d-24b0-4106-8813-b52610072b54",
  userSessionIssuerId: "a508b496-e74c-43de-8d1f-4da7479f03ee",
};
```

## Fields

| Field                                                                       | Type                                                                        | Required                                                                    | Description                                                                 |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `oauthProxyProviderId`                                                      | *string*                                                                    | :heavy_check_mark:                                                          | The gram-type oauth_proxy_provider whose Redis registrations are migrated.  |
| `userSessionIssuerId`                                                       | *string*                                                                    | :heavy_check_mark:                                                          | The target user_session_issuer the migrated user_session_clients attach to. |