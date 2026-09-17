import type {
  FilterChip,
  ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import {
  parseTargetFilter,
  selectedHookSources,
  selectedTargetValues,
  selectedUserEmails,
  toTargetTypes,
} from "@/components/observe/observeTargetFilters";
import { useSlugs } from "@/contexts/Sdk";
import type { GetToolUsageSummaryPayload } from "@gram/client/models/components/gettoolusagesummarypayload.js";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";
import { useMemo } from "react";

type ToolUsagePayloadInputs = {
  /** Applied filter chips, as `useObserveFilters` exposes them. */
  activeFilters: FilterChip[];
  /** Emails the selected roles resolve to; merged with the picked users. */
  roleEmails: string[];
  selectedHookTypes: ObserveTypeFilterValue[];
  accountType: string;
  /** Lowercased MCP client names; "unattributed" selects calls with none. */
  clientKeys?: string[];
  from: Date;
  to: Date;
};

export type ToolUsagePayload = {
  /** The payload every getToolUsage* endpoint takes. */
  summaryPayload: GetToolUsageSummaryPayload;
  /**
   * The react-query key fragment those endpoints share. Insights and Logs must
   * build it the same way: a byte-identical key is what lets a drill-down from
   * one page paint from the cache the other already filled.
   */
  sharedQueryKey: unknown[];
};

/**
 * Derives the shared tool-usage query inputs from the observe filter state.
 *
 * Both observe pages ask the same question of the same endpoints and differ
 * only in how they render the answer, so the derivation lives here rather than
 * being written out once per page — the two copies had already drifted in
 * their types and their memo dependencies.
 */
export function useToolUsagePayload({
  activeFilters,
  roleEmails,
  selectedHookTypes,
  accountType,
  clientKeys,
  from,
  to,
}: ToolUsagePayloadInputs): ToolUsagePayload {
  // Two organizations can both have a project called "default". Without the
  // tenant in the key, navigating between them reads the previous org's
  // telemetry out of the cache.
  const { orgSlug, projectSlug } = useSlugs();
  const serverFilters = useMemo(
    () => selectedTargetValues(activeFilters).map(parseTargetFilter),
    [activeFilters],
  );
  const hostedToolsetSlugs = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "hosted")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const shadowServerNames = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "shadow")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const metaMcpServerIds = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "gateway")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const userFilters = useMemo<ToolUsageUserFilter[]>(() => {
    const emails = [
      ...new Set([...selectedUserEmails(activeFilters), ...roleEmails]),
    ];
    return emails.map((email) => ({ kind: "email", key: email }));
  }, [activeFilters, roleEmails]);
  const hookSourceFilters = useMemo(
    () => selectedHookSources(activeFilters),
    [activeFilters],
  );

  // One normalization for the payload and the key: "no client filter" has to
  // be the same value in both, or Logs and Insights build different keys for
  // the same request and neither can paint from the other's cache.
  const normalizedClientKeys = useMemo(
    () => (clientKeys && clientKeys.length > 0 ? clientKeys : undefined),
    [clientKeys],
  );

  const summaryPayload = useMemo(
    () => ({
      from,
      to,
      hostedToolsetSlugs:
        hostedToolsetSlugs.length > 0 ? hostedToolsetSlugs : undefined,
      shadowServerNames:
        shadowServerNames.length > 0 ? shadowServerNames : undefined,
      metaMcpServerIds:
        metaMcpServerIds.length > 0 ? metaMcpServerIds : undefined,
      targetTypes: toTargetTypes(selectedHookTypes),
      userFilters: userFilters.length > 0 ? userFilters : undefined,
      hookSources: hookSourceFilters.length > 0 ? hookSourceFilters : undefined,
      accountType: accountType || undefined,
      clientKeys: normalizedClientKeys,
    }),
    [
      from,
      to,
      hostedToolsetSlugs,
      shadowServerNames,
      metaMcpServerIds,
      selectedHookTypes,
      userFilters,
      hookSourceFilters,
      accountType,
      normalizedClientKeys,
    ],
  );

  const sharedQueryKey = useMemo(
    () => [
      orgSlug,
      projectSlug,
      from.toISOString(),
      to.toISOString(),
      hostedToolsetSlugs,
      shadowServerNames,
      metaMcpServerIds,
      userFilters,
      hookSourceFilters,
      selectedHookTypes,
      accountType,
      normalizedClientKeys,
    ],
    [
      orgSlug,
      projectSlug,
      from,
      to,
      hostedToolsetSlugs,
      shadowServerNames,
      metaMcpServerIds,
      userFilters,
      hookSourceFilters,
      selectedHookTypes,
      accountType,
      normalizedClientKeys,
    ],
  );

  return { summaryPayload, sharedQueryKey };
}
