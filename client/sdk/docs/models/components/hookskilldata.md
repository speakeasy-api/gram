# HookSkillData

Skill activation payload.

## Example Usage

```typescript
import { HookSkillData } from "@gram/client/models/components/hookskilldata.js";

let value: HookSkillData = {
  name: "<value>",
};
```

## Fields

| Field                                    | Type                                     | Required                                 | Description                              |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `name`                                   | *string*                                 | :heavy_check_mark:                       | Activated skill name.                    |
| `source`                                 | *string*                                 | :heavy_minus_sign:                       | Skill source or namespace, if available. |