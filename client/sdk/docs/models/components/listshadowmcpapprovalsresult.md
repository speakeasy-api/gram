# ListShadowMCPApprovalsResult

## Example Usage

```typescript
import { ListShadowMCPApprovalsResult } from "@gram/client/models/components";

let value: ListShadowMCPApprovalsResult = {
  approvals: [],
};
```

## Fields

| Field       | Type                                                                           | Required           | Description                                                             |
| ----------- | ------------------------------------------------------------------------------ | ------------------ | ----------------------------------------------------------------------- |
| `approvals` | [components.ShadowMCPApproval](../../models/components/shadowmcpapproval.md)[] | :heavy_check_mark: | The approved shadow-MCP servers for the policy (URL- or command-keyed). |
