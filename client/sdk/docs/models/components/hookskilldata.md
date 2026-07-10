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

| Field    | Type     | Required           | Description                              |
| -------- | -------- | ------------------ | ---------------------------------------- |
| `name`   | _string_ | :heavy_check_mark: | Activated skill name.                    |
| `source` | _string_ | :heavy_minus_sign: | Skill source or namespace, if available. |
