# ListExternalCredentialsResult

## Example Usage

```typescript
import { ListExternalCredentialsResult } from "@gram/client/models/components/listexternalcredentialsresult.js";

let value: ListExternalCredentialsResult = {
  credentials: [
    {
      createdAt: new Date("2024-03-30T17:16:12.039Z"),
      id: "9843ac14-cf42-425f-9ce5-5a763d7cee2d",
      name: "<value>",
      organizationId: "<id>",
      provider: "aws_iam",
      updatedAt: new Date("2025-07-18T17:24:56.724Z"),
    },
  ],
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `credentials`                                                                                  | [components.ExternalCredentialSummary](../../models/components/externalcredentialsummary.md)[] | :heavy_check_mark:                                                                             | The organization's external credentials.                                                       |