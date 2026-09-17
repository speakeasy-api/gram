import { z } from "zod";

const day = z.iso.date();

export function usageRangeError(from: string, to: string): string | undefined {
  if (!day.safeParse(from).success || !day.safeParse(to).success) {
    return "Choose a start and end date.";
  }
  const start = new Date(`${from}T00:00:00Z`);
  const end = new Date(`${to}T00:00:00Z`);
  end.setUTCDate(end.getUTCDate() + 1);
  if (end <= start) return "End date must be on or after start date.";
  const lastDay = new Date(
    Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + 4, 0),
  ).getUTCDate();
  const maximum = new Date(
    Date.UTC(
      start.getUTCFullYear(),
      start.getUTCMonth() + 3,
      Math.min(start.getUTCDate(), lastDay),
    ),
  );
  if (end > maximum) return "Choose at most three calendar months.";
  return undefined;
}

const searchSchema = z.object({
  product: z
    .enum(["agent_session_storage", "mcp_bandwidth", "risk_content_scans"])
    .optional()
    .catch(undefined),
  interval: z.enum(["daily", "weekly", "monthly"]).optional().catch(undefined),
  cumulative: z.boolean().optional().catch(undefined),
  from: day.optional().catch(undefined),
  to: day.optional().catch(undefined),
});

export type BillingUsageSearch = z.infer<typeof searchSchema>;

export function billingUsageSearch(
  search: Record<string, unknown>,
): BillingUsageSearch {
  const parsed = searchSchema.parse(search);
  if (!parsed.from || !parsed.to || usageRangeError(parsed.from, parsed.to)) {
    return { ...parsed, from: undefined, to: undefined };
  }
  return parsed;
}

export function exclusiveEnd(day: string): string {
  const end = new Date(`${day}T00:00:00Z`);
  end.setUTCDate(end.getUTCDate() + 1);
  return end.toISOString();
}

export function inclusiveEnd(exclusive: string | Date): string {
  return new Date(new Date(exclusive).getTime() - 1).toISOString().slice(0, 10);
}
