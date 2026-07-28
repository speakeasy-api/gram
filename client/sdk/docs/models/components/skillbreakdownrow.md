# SkillBreakdownRow

Per-(skill, user) aggregated counts

## Example Usage

```typescript
import { SkillBreakdownRow } from "@gram/client/models/components/skillbreakdownrow.js";

let value: SkillBreakdownRow = {
  skillName: "<value>",
  useCount: 335310,
  userEmail: "<value>",
};
```

## Fields

| Field       | Type     | Required           | Description                               |
| ----------- | -------- | ------------------ | ----------------------------------------- |
| `skillName` | _string_ | :heavy_check_mark: | Skill name                                |
| `useCount`  | _number_ | :heavy_check_mark: | Use count for this skill/user combination |
| `userEmail` | _string_ | :heavy_check_mark: | User email address                        |
