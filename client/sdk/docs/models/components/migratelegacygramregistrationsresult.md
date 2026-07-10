# MigrateLegacyGramRegistrationsResult

Result of a legacy gram registration migration.

## Example Usage

```typescript
import { MigrateLegacyGramRegistrationsResult } from "@gram/client/models/components/migratelegacygramregistrationsresult.js";

let value: MigrateLegacyGramRegistrationsResult = {
  migratedCount: 867584,
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `migratedCount`                                                                              | *number*                                                                                     | :heavy_check_mark:                                                                           | Number of user_session_clients newly inserted; already-migrated registrations count as zero. |