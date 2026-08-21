import {
  buildProjectOverviewQuery,
  type ProjectOverviewScope,
} from "@/components/project/projectOverviewQuery";
import { useSlugs } from "@/contexts/Sdk";
import type { GetProjectOverviewResult } from "@gram/client/models/components/getprojectoverviewresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";

/**
 * Start of the window the gate asks about. The dashboard's presets stop at 90
 * days, and the client-side project record carries no `createdAt` (only
 * id/name/slug reach the browser), so the range is anchored to a date that
 * predates any Gram telemetry instead of to project creation.
 */
export const PROJECT_GUIDE_OVERVIEW_FROM = "2024-01-01T00:00:00.000Z";

/**
 * The overview query key contains its range, so an exact `now` would mint a new
 * cache entry on every render. The end of the window is rounded UP to this
 * bucket: stable key for a minute, and nothing that just happened falls outside
 * the window (rounding down would hide a tool call made seconds ago).
 */
const PROJECT_GUIDE_OVERVIEW_BUCKET_MS = 60_000;

export function allTimeOverviewScope(args: {
  organization: string;
  project: string;
  now: Date;
}): ProjectOverviewScope {
  const bucketed =
    Math.floor(args.now.getTime() / PROJECT_GUIDE_OVERVIEW_BUCKET_MS + 1) *
    PROJECT_GUIDE_OVERVIEW_BUCKET_MS;

  return {
    organization: args.organization,
    project: args.project,
    range: {
      from: PROJECT_GUIDE_OVERVIEW_FROM,
      to: new Date(bucketed).toISOString(),
    },
  };
}

/** An unreadable overview is not proof that the project is empty. */
export function isOverviewEmpty(
  overview: GetProjectOverviewResult | undefined,
): boolean {
  if (!overview) return false;
  const { activeServersCount, totalToolCalls, totalChats } = overview.summary;
  return activeServersCount === 0 && totalToolCalls === 0 && totalChats === 0;
}

/**
 * The all-time overview under its own query key, so it never collides with the
 * dashboard's range-scoped copy.
 */
export function useAllTimeProjectOverview(args: { enabled: boolean }): {
  data: GetProjectOverviewResult | undefined;
  isPending: boolean;
  isError: boolean;
} {
  const { orgSlug, projectSlug } = useSlugs();
  const client = useGramContext();

  const { data, isPending, isError } = useQuery({
    ...buildProjectOverviewQuery(
      client,
      allTimeOverviewScope({
        organization: orgSlug ?? "",
        project: projectSlug ?? "",
        now: new Date(),
      }),
    ),
    enabled: args.enabled && Boolean(orgSlug) && Boolean(projectSlug),
    throwOnError: false,
  });

  return { data, isPending, isError };
}
