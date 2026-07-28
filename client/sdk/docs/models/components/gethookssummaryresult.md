# GetHooksSummaryResult

Result of hooks summary query

## Example Usage

```typescript
import { GetHooksSummaryResult } from "@gram/client/models/components/gethookssummaryresult.js";

let value: GetHooksSummaryResult = {
  breakdown: [],
  servers: [
    {
      eventCount: 886310,
      failureCount: 282525,
      failureRate: 5103.91,
      serverName: "<value>",
      successCount: 102046,
      uniqueTools: 244548,
    },
  ],
  skillBreakdown: [
    {
      skillName: "<value>",
      useCount: 678565,
      userEmail: "<value>",
    },
  ],
  skillTimeSeries: [
    {
      bucketStartNs: "<value>",
      eventCount: 280257,
      skillName: "<value>",
    },
  ],
  skills: [
    {
      skillName: "<value>",
      uniqueUsers: 884610,
      useCount: 482472,
    },
  ],
  timeSeries: [],
  totalEvents: 147350,
  totalSessions: 103664,
  users: [
    {
      eventCount: 525906,
      failureCount: 19982,
      failureRate: 3982.49,
      successCount: 239243,
      uniqueTools: 419834,
      userEmail: "<value>",
    },
  ],
};
```

## Fields

| Field             | Type                                                                                 | Required           | Description                                                    |
| ----------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------------------------------------------------------- |
| `breakdown`       | [components.HooksBreakdownRow](../../models/components/hooksbreakdownrow.md)[]       | :heavy_check_mark: | Cross-dimensional pivot: (user, server, source, tool) x counts |
| `servers`         | [components.HooksServerSummary](../../models/components/hooksserversummary.md)[]     | :heavy_check_mark: | Aggregated metrics grouped by server                           |
| `skillBreakdown`  | [components.SkillBreakdownRow](../../models/components/skillbreakdownrow.md)[]       | :heavy_check_mark: | Per-user skill breakdown                                       |
| `skillTimeSeries` | [components.SkillTimeSeriesPoint](../../models/components/skilltimeseriespoint.md)[] | :heavy_check_mark: | Time-bucketed event counts by skill                            |
| `skills`          | [components.SkillSummary](../../models/components/skillsummary.md)[]                 | :heavy_check_mark: | Aggregated metrics grouped by skill                            |
| `timeSeries`      | [components.HooksTimeSeriesPoint](../../models/components/hookstimeseriespoint.md)[] | :heavy_check_mark: | Time-bucketed event counts by server and user                  |
| `totalEvents`     | _number_                                                                             | :heavy_check_mark: | Total number of hook events                                    |
| `totalSessions`   | _number_                                                                             | :heavy_check_mark: | Total number of unique sessions                                |
| `users`           | [components.HooksUserSummary](../../models/components/hooksusersummary.md)[]         | :heavy_check_mark: | Aggregated metrics grouped by user                             |
