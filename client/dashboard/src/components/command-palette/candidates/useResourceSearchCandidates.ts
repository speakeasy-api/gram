import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useRef } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

export const COMMAND_SEARCH_SCHEMA_VERSION = "1";
const PAYLOAD_BUCKET_CEILING = 250_000;

function canonicalJson(value: unknown): string {
  if (value === null) return "null";
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJson).sort().join(",")}]`;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value)
      .filter(([, entry]) => entry !== undefined)
      .sort(([left], [right]) => left.localeCompare(right));
    return `{${entries
      .map(([key, entry]) => `${JSON.stringify(key)}:${canonicalJson(entry)}`)
      .join(",")}}`;
  }
  return JSON.stringify(value) ?? "null";
}

/** Exact, deterministic and local-only cache partition for effective grants. */
export function createGrantFingerprint(grants: readonly unknown[]): string {
  return canonicalJson(grants);
}

type PayloadByteBucket =
  | "under_10k"
  | "10k_to_50k"
  | "50k_to_250k"
  | "over_250k";

export const MIGRATED_RESOURCE_KINDS = [
  "assistant",
  "environment",
  "gateway",
  "mcp",
  "plugin",
  "rule",
  "skill",
] as const;
type MigratedResourceKind = (typeof MIGRATED_RESOURCE_KINDS)[number];

interface ResourceSearchItem {
  id: string;
  kind: string;
  name: string;
  routeSelector: string;
  /** Compact server-provided aliases used for local matching. */
  keywords?: readonly string[];
}

export interface ResourceSearchMetrics {
  state: "idle" | "loading" | "ready" | "fallback";
  searchRequestCount: number;
  searchInFlight: boolean;
  candidateCount: number;
  complete: boolean;
  truncated: boolean;
  truncatedKindCount: number;
  payloadByteBucket: PayloadByteBucket | null;
  parseSearchDurationMs: number | null;
}

export interface ResourceSearchCandidatesResult {
  candidates: LauncherCandidate[];
  fallbackKinds: ReadonlySet<MigratedResourceKind>;
  metrics: ResourceSearchMetrics;
}

const EMPTY_CANDIDATES: LauncherCandidate[] = [];
const EMPTY_FALLBACK_KINDS: ReadonlySet<MigratedResourceKind> = new Set();
const ALL_FALLBACK_KINDS: ReadonlySet<MigratedResourceKind> = new Set(
  MIGRATED_RESOURCE_KINDS,
);
const EMPTY_METRICS: ResourceSearchMetrics = {
  state: "idle",
  searchRequestCount: 0,
  searchInFlight: false,
  candidateCount: 0,
  complete: false,
  truncated: false,
  truncatedKindCount: 0,
  payloadByteBucket: null,
  parseSearchDurationMs: null,
};

function payloadBucket(bytes: number): PayloadByteBucket {
  if (bytes < 10_000) return "under_10k";
  if (bytes < 50_000) return "10k_to_50k";
  if (bytes < PAYLOAD_BUCKET_CEILING) return "50k_to_250k";
  return "over_250k";
}

function boundedUtf8Bytes(value: string, remaining: number): number {
  if (value.length >= remaining) return remaining;
  return Math.min(new TextEncoder().encode(value).byteLength, remaining);
}

/**
 * The generated response does not currently expose wire bytes. Estimate only
 * the compact fields and stop at the largest bucket boundary, avoiding a
 * second full-payload JSON allocation for telemetry.
 */
function estimatePayloadBytes(items: readonly ResourceSearchItem[]): number {
  let bytes = 64;
  for (const item of items) {
    bytes = Math.min(PAYLOAD_BUCKET_CEILING, bytes + 32);
    for (const value of [
      item.id,
      item.kind,
      item.name,
      item.routeSelector,
      ...(item.keywords ?? []),
    ]) {
      bytes += boundedUtf8Bytes(value, PAYLOAD_BUCKET_CEILING - bytes);
      if (bytes >= PAYLOAD_BUCKET_CEILING) return bytes;
    }
  }
  return bytes;
}

const KIND_PRESENTATION: Record<
  MigratedResourceKind,
  { detail: string; group: string; icon: LauncherCandidate["icon"] }
> = {
  assistant: { detail: "Assistant", group: "Assistants", icon: "bot" },
  environment: { detail: "Environment", group: "Environments", icon: "blocks" },
  gateway: { detail: "MCP gateway", group: "Gateways", icon: "network" },
  mcp: { detail: "MCP server", group: "MCP Servers", icon: "network" },
  plugin: { detail: "Plugin", group: "Plugins", icon: "puzzle" },
  rule: {
    detail: "Detection rule",
    group: "Detection Rules",
    icon: "scan-search",
  },
  skill: { detail: "Skill", group: "Skills", icon: "terminal" },
};

function validField(value: unknown): value is string {
  return (
    typeof value === "string" && value.trim().length > 0 && value.length <= 1024
  );
}

/** Maps only understood, well-formed server records to canonical dashboard routes. */
export function mapResourceSearchItems(
  items: readonly ResourceSearchItem[],
  routes: ReturnType<typeof useRoutes>,
  navigate: ReturnType<typeof useNavigate>,
): LauncherCandidate[] {
  return items.flatMap((item): LauncherCandidate[] => {
    if (!MIGRATED_RESOURCE_KINDS.includes(item.kind as MigratedResourceKind)) {
      return [];
    }
    const kind = item.kind as MigratedResourceKind;
    const presentation = KIND_PRESENTATION[kind];
    if (
      !validField(item.id) ||
      !validField(item.name) ||
      !validField(item.routeSelector)
    ) {
      return [];
    }

    const selector = item.routeSelector;
    let run: () => void;
    switch (kind) {
      case "assistant":
        run = () => routes.assistants.detail.goTo(selector);
        break;
      case "environment":
        run = () => routes.environments.environment.goTo(selector);
        break;
      case "gateway":
        run = () => routes.mcp.gateway.overview.goTo(selector);
        break;
      case "mcp":
        run = () => routes.mcp.details.overview.goTo(selector);
        break;
      case "plugin":
        run = () => routes.plugins.detail.goTo(selector);
        break;
      case "rule":
        run = () => {
          void navigate(
            `${routes.policyCenter.href()}?tab=detection-rules&rule=${encodeURIComponent(selector)}`,
          );
        };
        break;
      case "skill":
        run = () => routes.skills.detail.overview.goTo(selector);
        break;
    }

    return [
      {
        id: `${kind}:${item.id}`,
        kind: kind === "mcp" ? "mcp_server" : kind,
        title: item.name,
        detail: presentation.detail,
        keywords: [
          ...new Set([
            kind,
            selector,
            item.id,
            ...(item.keywords ?? []).filter(validField).slice(0, 32),
          ]),
        ],
        resourceIdentity: { id: item.id, routeSelector: selector },
        verbs: ["open"],
        icon: presentation.icon,
        group: presentation.group,
        run,
      },
    ];
  });
}

function migratedKinds(values: readonly string[] | undefined) {
  const known = new Set<string>(MIGRATED_RESOURCE_KINDS);
  return new Set(
    (values ?? []).filter((value): value is MigratedResourceKind =>
      known.has(value),
    ),
  );
}

/** Searches only after the open palette's debounced query has two characters. */
export function useResourceSearchCandidates({
  enabled,
  accessKey,
  query: searchQuery,
}: {
  enabled: boolean;
  accessKey: string;
  query: string;
}): ResourceSearchCandidatesResult {
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const gramProject = useProjectSlugForRequests();
  const organization = useOrganization();
  const project = useProject();
  const session = useSession();
  const requestCount = useRef(0);
  const wasEnabled = useRef(false);
  if (enabled && !wasEnabled.current) requestCount.current = 0;
  wasEnabled.current = enabled;

  const normalizedQuery = searchQuery.trim();
  const queryCodePointLength = Array.from(normalizedQuery).length;
  const searchEnabled =
    enabled && queryCodePointLength >= 2 && queryCodePointLength <= 128;
  const queryKey = useMemo(
    () =>
      [
        "command-resource-search",
        session.user.id,
        organization.id,
        project.id,
        gramProject,
        normalizedQuery,
        COMMAND_SEARCH_SCHEMA_VERSION,
        accessKey,
      ] as const,
    [
      accessKey,
      gramProject,
      normalizedQuery,
      organization.id,
      project.id,
      session.user.id,
    ],
  );

  const result = useQuery({
    queryKey,
    queryFn: ({ signal }) => {
      requestCount.current += 1;
      return client.commandSearch.searchResources(
        { gramProject, q: normalizedQuery },
        undefined,
        { signal },
      );
    },
    enabled: searchEnabled,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    throwOnError: false,
  });

  const searchInFlight = searchEnabled && result.fetchStatus === "fetching";
  const indexed = useMemo(() => {
    // React Query may retain the previous key's data while the next request is
    // in flight. Never expose it under the new query.
    if (!searchEnabled || searchInFlight || !result.data) return null;
    const data = result.data;
    if (data.schemaVersion !== COMMAND_SEARCH_SCHEMA_VERSION) return null;
    const startedAt = performance.now();
    const candidates = mapResourceSearchItems(data.items, routes, navigate);
    return {
      candidates,
      failedKinds: migratedKinds(data.failedKinds),
      partial: data.partial === true,
      timedOut: data.timedOut === true,
      truncated: data.truncated === true,
      truncatedKinds: migratedKinds(data.truncatedKinds),
      parseSearchDurationMs: performance.now() - startedAt,
      bytes: estimatePayloadBytes(data.items),
    };
  }, [navigate, result.data, routes, searchEnabled, searchInFlight]);

  if (indexed) {
    return {
      candidates: indexed.candidates,
      fallbackKinds: indexed.failedKinds,
      metrics: {
        state:
          indexed.timedOut || indexed.failedKinds.size > 0
            ? "fallback"
            : "ready",
        searchRequestCount: requestCount.current,
        searchInFlight: false,
        candidateCount: indexed.candidates.length,
        complete: !indexed.partial && !indexed.truncated,
        truncated: indexed.truncated,
        truncatedKindCount: indexed.truncatedKinds.size,
        payloadByteBucket: payloadBucket(indexed.bytes),
        parseSearchDurationMs: indexed.parseSearchDurationMs,
      },
    };
  }

  if (
    searchEnabled &&
    !searchInFlight &&
    (result.isError || result.data !== undefined)
  ) {
    return {
      candidates: EMPTY_CANDIDATES,
      fallbackKinds: ALL_FALLBACK_KINDS,
      metrics: {
        ...EMPTY_METRICS,
        state: "fallback",
        searchRequestCount: requestCount.current,
        searchInFlight: false,
      },
    };
  }

  return {
    candidates: EMPTY_CANDIDATES,
    fallbackKinds: EMPTY_FALLBACK_KINDS,
    metrics: searchEnabled
      ? {
          ...EMPTY_METRICS,
          state: "loading",
          searchRequestCount: requestCount.current,
          searchInFlight,
        }
      : EMPTY_METRICS,
  };
}
