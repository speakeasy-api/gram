import { z } from "zod";

import { gramAdminFetch } from "@/lib/gramAdminApi";

const versionStatus = z.enum(["current", "behind", "unsupported", "unknown"]);

const bucketSchema = z.object({
  value: z.string(),
  installs: z.number(),
  events: z.number(),
});

const installSchema = z.object({
  project_id: z.string(),
  user_email: z.string(),
  hostname: z.string(),
  harness: z.string(),
  version: z.string(),
  version_status: versionStatus,
  endpoint_generation: z.string(),
  install_route: z.string(),
  first_seen: z.string(),
  last_seen: z.string(),
  events: z.number(),
  errors: z.number(),
  quiet: z.boolean(),
});

const hookFleetSchema = z.object({
  organization_id: z.string(),
  window_days: z.number(),
  since: z.string(),
  latest_version: z.string(),
  minimum_version: z.string().optional(),
  quiet_after_days: z.number(),
  truncated: z.boolean(),
  total_installs: z.number(),
  quiet_installs: z.number(),
  legacy_events: z.number(),
  versions: z.array(
    z.object({
      version: z.string(),
      status: versionStatus,
      installs: z.number(),
    }),
  ),
  routes: z.array(bucketSchema),
  generations: z.array(bucketSchema),
  installs: z.array(installSchema),
});

export type HookFleet = z.infer<typeof hookFleetSchema>;
export type HookFleetInstall = z.infer<typeof installSchema>;
export type HookFleetBucket = z.infer<typeof bucketSchema>;
export type HookVersionStatus = z.infer<typeof versionStatus>;

export function hookFleetQuery(organizationId: string): {
  queryKey: string[];
  queryFn: () => Promise<HookFleet>;
  staleTime: number;
} {
  return {
    queryKey: ["hook-fleet", organizationId],
    queryFn: async (): Promise<HookFleet> =>
      hookFleetSchema.parse(
        await gramAdminFetch<unknown>(
          `/admin/organizationHookFleet.get?organization_id=${encodeURIComponent(organizationId)}`,
          { cache: "no-store" },
        ),
      ),
    staleTime: 60_000,
  };
}
