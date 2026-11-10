import { z } from "zod";

import { gramAdminFetch } from "@/lib/gramAdminApi";

const cellSchema = z.object({
  capability: z.enum(["session", "blocking", "identity", "cost", "shadow"]),
  surface: z.enum([
    "mcp_gateway",
    "claude_code",
    "claude_chat",
    "cowork",
    "codex",
    "cursor",
    "other",
  ]),
  status: z.enum(["observed", "none", "pending", "na"]),
  value: z.number(),
  /** Overrides the capability's unit when the column counts something else. */
  unit: z.string().default(""),
  detail: z.string(),
  last_seen: z.string().optional(),
});

const coverageSchema = z.object({
  cells: z.array(cellSchema),
  unmapped: z.array(
    z.object({ hook_source: z.string(), sessions: z.number() }),
  ),
  window_days: z.number(),
  from: z.string(),
  to: z.string(),
});

export type CoverageCell = z.infer<typeof cellSchema>;
export type Coverage = z.infer<typeof coverageSchema>;
export type SurfaceId = CoverageCell["surface"];
export type CapabilityId = CoverageCell["capability"];

export function supportCoverageQuery(organizationId: string): {
  queryKey: string[];
  queryFn: () => Promise<Coverage>;
  staleTime: number;
} {
  return {
    queryKey: ["support-coverage", organizationId],
    queryFn: async (): Promise<Coverage> =>
      coverageSchema.parse(
        await gramAdminFetch<unknown>(
          `/admin/supportCoverage.get?organization_id=${encodeURIComponent(organizationId)}`,
          { cache: "no-store" },
        ),
      ),
    staleTime: 60_000,
  };
}
