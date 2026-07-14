# RiskPolicyStatus

## Example Usage

```typescript
import { RiskPolicyStatus } from "@gram/client/models/components/riskpolicystatus.js";

let value: RiskPolicyStatus = {
  analyzedMessages: 226498,
  findingsCount: 848103,
  pendingMessages: 935058,
  policyId: "89c274df-7040-48fc-b548-cecdd5791d91",
  policyVersion: 132856,
  totalMessages: 134500,
  workflowStatus: "not_started",
};
```

## Fields

| Field              | Type                                                                   | Required           | Description                                        |
| ------------------ | ---------------------------------------------------------------------- | ------------------ | -------------------------------------------------- |
| `analyzedMessages` | _number_                                                               | :heavy_check_mark: | Messages analyzed at the current policy version.   |
| `findingsCount`    | _number_                                                               | :heavy_check_mark: | Number of findings at the current policy version.  |
| `pendingMessages`  | _number_                                                               | :heavy_check_mark: | Messages not yet analyzed.                         |
| `policyId`         | _string_                                                               | :heavy_check_mark: | The risk policy ID.                                |
| `policyVersion`    | _number_                                                               | :heavy_check_mark: | Current policy version.                            |
| `totalMessages`    | _number_                                                               | :heavy_check_mark: | Total messages in the project.                     |
| `workflowStatus`   | [components.WorkflowStatus](../../models/components/workflowstatus.md) | :heavy_check_mark: | Workflow state: running, sleeping, or not_started. |
