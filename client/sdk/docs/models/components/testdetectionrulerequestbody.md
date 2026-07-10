# TestDetectionRuleRequestBody

## Example Usage

```typescript
import { TestDetectionRuleRequestBody } from "@gram/client/models/components/testdetectionrulerequestbody.js";

let value: TestDetectionRuleRequestBody = {
  ruleId: "<id>",
  text: "<value>",
};
```

## Fields

| Field                                                                                                   | Type                                                                                                    | Required                                                                                                | Description                                                                                             |
| ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `detectionExpr`                                                                                         | *string*                                                                                                | :heavy_minus_sign:                                                                                      | CEL detection predicate for `custom.*` rule ids, evaluated against the sample message.                  |
| `ruleId`                                                                                                | *string*                                                                                                | :heavy_check_mark:                                                                                      | Rule identifier to evaluate (e.g. `secret.aws_access_token`, `pii.email_address`, `custom.acme_token`). |
| `text`                                                                                                  | *string*                                                                                                | :heavy_check_mark:                                                                                      | Sample text to scan.                                                                                    |