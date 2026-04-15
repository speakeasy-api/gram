export type EventType =
  | "tool_argument"
  | "tool_output"
  | "user_message"
  | "assistant_message";

export type DlpCategory =
  | "secret"
  | "financial"
  | "government_id"
  | "healthcare"
  | "contact_info";

export type Severity = "low" | "medium" | "high" | "critical";

export type DlpEvent = {
  id: string;
  timestamp: Date;
  userId: string;
  userName: string;
  eventType: EventType;
  category: DlpCategory;
  ruleName: string;
  severity: Severity;
  contentPreview: string;
  chatSessionId: string;
};

const USERS = [
  { id: "u1", name: "Alice Chen" },
  { id: "u2", name: "Bob Martinez" },
  { id: "u3", name: "Carol Johnson" },
  { id: "u4", name: "David Kim" },
  { id: "u5", name: "Eve Patel" },
  { id: "u6", name: "Frank Wu" },
];

const RULES: Array<{
  category: DlpCategory;
  ruleName: string;
  severity: Severity;
  preview: string;
}> = [
  {
    category: "secret",
    ruleName: "AWS Access Key",
    severity: "critical",
    preview: "AKIA***************EXAMPLE",
  },
  {
    category: "secret",
    ruleName: "GitHub Token",
    severity: "critical",
    preview: "ghp_****************************",
  },
  {
    category: "secret",
    ruleName: "Anthropic API Key",
    severity: "critical",
    preview: "sk-ant-api03-****...",
  },
  {
    category: "financial",
    ruleName: "Visa Card Number",
    severity: "high",
    preview: "4111 **** **** 1111",
  },
  {
    category: "financial",
    ruleName: "IBAN Code",
    severity: "high",
    preview: "GB29 NWBK 6016 1331 9268 19",
  },
  {
    category: "financial",
    ruleName: "US Bank Account",
    severity: "high",
    preview: "Account: ****4567, Routing: 021000021",
  },
  {
    category: "government_id",
    ruleName: "US SSN",
    severity: "critical",
    preview: "SSN: ***-**-1234",
  },
  {
    category: "government_id",
    ruleName: "US Passport",
    severity: "critical",
    preview: "Passport: *****6789",
  },
  {
    category: "government_id",
    ruleName: "US Driver License",
    severity: "high",
    preview: "DL: D***-****-1234",
  },
  {
    category: "healthcare",
    ruleName: "Medical Record Number",
    severity: "high",
    preview: "MRN: ****5678",
  },
  {
    category: "healthcare",
    ruleName: "Medicare Beneficiary ID",
    severity: "high",
    preview: "MBI: 1EG4-TE5-MK72",
  },
  {
    category: "contact_info",
    ruleName: "Email Address",
    severity: "low",
    preview: "j***@example.com",
  },
  {
    category: "contact_info",
    ruleName: "Phone Number",
    severity: "medium",
    preview: "+1 (555) ***-****",
  },
  {
    category: "contact_info",
    ruleName: "Person Name + Address",
    severity: "medium",
    preview: "John Doe, 123 Main St...",
  },
];

const EVENT_TYPES: EventType[] = [
  "tool_argument",
  "tool_output",
  "user_message",
  "assistant_message",
];

function seededRandom(seed: number) {
  let s = seed;
  return () => {
    s = (s * 16807 + 0) % 2147483647;
    return (s - 1) / 2147483646;
  };
}

export function generateMockEvents(count: number = 200): DlpEvent[] {
  const rand = seededRandom(42);
  const now = new Date();
  const events: DlpEvent[] = [];

  for (let i = 0; i < count; i++) {
    const daysAgo = Math.floor(rand() * 30);
    const hoursAgo = Math.floor(rand() * 24);
    const timestamp = new Date(now);
    timestamp.setDate(timestamp.getDate() - daysAgo);
    timestamp.setHours(timestamp.getHours() - hoursAgo);

    const user = USERS[Math.floor(rand() * USERS.length)];
    const rule = RULES[Math.floor(rand() * RULES.length)];
    const eventType = EVENT_TYPES[Math.floor(rand() * EVENT_TYPES.length)];

    const sessionIndex = Math.floor(rand() * 30);
    events.push({
      id: `dlp-${i}`,
      timestamp,
      userId: user.id,
      userName: user.name,
      eventType,
      category: rule.category,
      ruleName: rule.ruleName,
      severity: rule.severity,
      contentPreview: rule.preview,
      chatSessionId: `chat-${sessionIndex.toString().padStart(3, "0")}`,
    });
  }

  return events.sort((a, b) => b.timestamp.getTime() - a.timestamp.getTime());
}

export type DailyTimeSeries = {
  date: string;
  flagged: number;
  total: number;
};

export function generateTimeSeries(events: DlpEvent[]): DailyTimeSeries[] {
  const now = new Date();
  const series: DailyTimeSeries[] = [];

  const rand = seededRandom(99);

  for (let i = 29; i >= 0; i--) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    const dateStr = d.toISOString().slice(0, 10);

    const dayEvents = events.filter(
      (e) => e.timestamp.toISOString().slice(0, 10) === dateStr,
    );

    const baseTotal = 80 + Math.floor(rand() * 120);

    series.push({
      date: dateStr,
      flagged: dayEvents.length,
      total: baseTotal + dayEvents.length,
    });
  }

  return series;
}

export type UserRisk = {
  userId: string;
  userName: string;
  flaggedCount: number;
  lastFlagged: Date;
  topCategory: DlpCategory;
};

export function computeUserRisks(events: DlpEvent[]): UserRisk[] {
  const byUser = new Map<
    string,
    {
      name: string;
      count: number;
      lastFlagged: Date;
      categories: Map<DlpCategory, number>;
    }
  >();

  for (const event of events) {
    const existing = byUser.get(event.userId);
    if (existing) {
      existing.count++;
      if (event.timestamp > existing.lastFlagged) {
        existing.lastFlagged = event.timestamp;
      }
      existing.categories.set(
        event.category,
        (existing.categories.get(event.category) ?? 0) + 1,
      );
    } else {
      const categories = new Map<DlpCategory, number>();
      categories.set(event.category, 1);
      byUser.set(event.userId, {
        name: event.userName,
        count: 1,
        lastFlagged: event.timestamp,
        categories,
      });
    }
  }

  return Array.from(byUser.entries())
    .map(([userId, data]) => {
      let topCategory: DlpCategory = "contact_info";
      let topCount = 0;
      for (const [cat, count] of data.categories) {
        if (count > topCount) {
          topCount = count;
          topCategory = cat;
        }
      }
      return {
        userId,
        userName: data.name,
        flaggedCount: data.count,
        lastFlagged: data.lastFlagged,
        topCategory,
      };
    })
    .sort((a, b) => b.flaggedCount - a.flaggedCount);
}

export function computeCategoryBreakdown(
  events: DlpEvent[],
): Array<{ category: DlpCategory; count: number }> {
  const counts = new Map<DlpCategory, number>();
  for (const event of events) {
    counts.set(event.category, (counts.get(event.category) ?? 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([category, count]) => ({ category, count }))
    .sort((a, b) => b.count - a.count);
}
