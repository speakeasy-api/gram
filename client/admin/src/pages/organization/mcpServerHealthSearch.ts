import { z } from "zod";

export const HEALTH_WINDOWS = [14, 30, 90] as const;
export type HealthWindow = (typeof HEALTH_WINDOWS)[number];

const searchSchema = z.object({
  // Kept so Back and the crumb return to the list with this project selected.
  project: z.string().optional().catch(undefined),
  window: z.coerce
    .number()
    // From the same list the picker draws, so the two cannot drift.
    .pipe(z.literal(HEALTH_WINDOWS))
    .optional()
    .catch(undefined),
});

export type McpServerHealthSearch = z.infer<typeof searchSchema>;

export function mcpServerHealthSearch(
  search: Record<string, unknown>,
): McpServerHealthSearch {
  return searchSchema.parse(search);
}
