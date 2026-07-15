import { getPresetRange, type DateRangePreset } from "@gram-ai/elements";
import { telemetryGetProjectOverview } from "@gram/client/funcs/telemetryGetProjectOverview";
import type { GetProjectOverviewResult } from "@gram/client/models/components/getprojectoverviewresult.js";
import { unwrapAsync } from "@gram/client/types/fp";
import type { QueryKey } from "@tanstack/react-query";

/**
 * Presets are keyed by name, not resolved timestamps: timestamp keys would
 * never match across mounts. Custom ranges use their exact ISO bounds.
 */
type ProjectOverviewRange =
  | { preset: DateRangePreset }
  | { from: string; to: string };

export type ProjectOverviewScope = {
  organization: string;
  project: string;
  range: ProjectOverviewRange;
};

// Product decision: an overview up to 30s old may render without a refetch,
// so a prefetch from org home survives navigation.
export const PROJECT_OVERVIEW_STALE_TIME_MS = 30_000;

const KEY_PREFIX = ["project", "overview"] as const;

export type ProjectOverviewQueryKey = readonly [
  string,
  string,
  ProjectOverviewScope,
];

export function projectOverviewQueryKey(
  scope: ProjectOverviewScope,
): ProjectOverviewQueryKey {
  return [...KEY_PREFIX, scope];
}

/**
 * Overview keys are org/project-scoped, so SdkProvider's project-switch
 * invalidation skips them; otherwise it would discard the prefetch.
 */
export function isProjectOverviewQueryKey(queryKey: QueryKey): boolean {
  return queryKey[0] === KEY_PREFIX[0] && queryKey[1] === KEY_PREFIX[1];
}

function resolveRange(range: ProjectOverviewRange): { from: Date; to: Date } {
  if ("preset" in range) {
    return getPresetRange(range.preset);
  }
  return { from: new Date(range.from), to: new Date(range.to) };
}

type OverviewClient = Parameters<typeof telemetryGetProjectOverview>[0];

/** Shared by the org-home prefetch and ProjectDashboard so keys never drift. */
export function buildProjectOverviewQuery(
  client: OverviewClient,
  scope: ProjectOverviewScope,
): {
  queryKey: ProjectOverviewQueryKey;
  queryFn: () => Promise<GetProjectOverviewResult>;
  staleTime: number;
} {
  return {
    queryKey: projectOverviewQueryKey(scope),
    queryFn: () => {
      const { from, to } = resolveRange(scope.range);
      return unwrapAsync(
        telemetryGetProjectOverview(client, {
          // Explicit slug: org routes bind the SDK fetcher to `default`, and
          // the request must match the project in the cache key.
          gramProject: scope.project,
          getProjectMetricsSummaryPayload: { from, to },
        }),
      );
    },
    staleTime: PROJECT_OVERVIEW_STALE_TIME_MS,
  };
}
