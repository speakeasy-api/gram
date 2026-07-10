# SkillSummary

Aggregated skills metrics for a single skill

## Example Usage

```typescript
import { SkillSummary } from "@gram/client/models/components/skillsummary.js";

let value: SkillSummary = {
  skillName: "<value>",
  uniqueUsers: 372919,
  useCount: 686333,
};
```

## Fields

| Field                                      | Type                                       | Required                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `skillName`                                | *string*                                   | :heavy_check_mark:                         | Skill name (extracted from tool name)      |
| `uniqueUsers`                              | *number*                                   | :heavy_check_mark:                         | Number of unique users who used this skill |
| `useCount`                                 | *number*                                   | :heavy_check_mark:                         | Total number of times this skill was used  |