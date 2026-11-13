import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { PROJECT_RESOURCE_INDEX_QUERY_PREFIX } from "@/lib/project-resource-index-cache";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useRef } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

const PROJECT_RESOURCE_INDEX_SCHEMA_VERSION = "1";
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

const MIGRATED_RESOURCE_KINDS = [
  "assistant",
  "environment",
  "gateway",
  "mcp",
  "plugin",
  "rule",
  "skill",
] as const;
type MigratedResourceKind = (typeof MIGRATED_RESOURCE_KINDS)[number];

interface ResourceIndexItem {
  id: string;
  kind: string;
  name: string;
  routeSelector: string;
  /** Compact server-provided aliases used for local matching. */
  keywords?: readonly string[];
}

export interface ProjectResourceIndexMetrics {
  state: "idle" | "loading" | "ready" | "fallback";
  indexRequestCount: number;
  indexInFlight: boolean;
  cacheHit: boolean;
  itemCount: number;
  complete: boolean;
  incompleteKindCount: number;
  payloadByteBucket: PayloadByteBucket | null;
  mappingDurationMs: number | null;
}

export interface ProjectResourceIndexCandidatesResult {
  candidates: LauncherCandidate[];
  fallbackKinds: ReadonlySet<MigratedResourceKind>;
  metrics: ProjectResourceIndexMetrics;
}

const EMPTY_CANDIDATES: LauncherCandidate[] = [];
const EMPTY_FALLBACK_KINDS: ReadonlySet<MigratedResourceKind> = new Set();
const ALL_FALLBACK_KINDS: ReadonlySet<MigratedResourceKind> = new Set(
  MIGRATED_RESOURCE_KINDS,
);
const EMPTY_METRICS: ProjectResourceIndexMetrics = {
  state: "idle",
  indexRequestCount: 0,
  indexInFlight: false,
  cacheHit: false,
  itemCount: 0,
  complete: false,
  incompleteKindCount: 0,
  payloadByteBucket: null,
  mappingDurationMs: null,
};

/** Cached rows must not mask a settled refresh failure. */
export function indexErrorRequiresLegacyFallback(
  enabled: boolean,
  indexInFlight: boolean,
  isError: boolean,
): boolean {
  return enabled && !indexInFlight && isError;
}

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
function estimatePayloadBytes(items: readonly ResourceIndexItem[]): number {
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
function mapResourceIndexItems(
  items: readonly ResourceIndexItem[],
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

function knownKinds(
  values:
    | readonly { kind: string; complete: boolean; overflow: boolean }[]
    | undefined,
): Map<MigratedResourceKind, { complete: boolean; overflow: boolean }> {
  const known = new Set<string>(MIGRATED_RESOURCE_KINDS);
  return new Map(
    (values ?? [])
      .filter((value) => known.has(value.kind))
      .map((value) => [value.kind as MigratedResourceKind, value]),
  );
}

function projectResourceIndexQueryKey(parts: {
  userId: string;
  organizationId: string;
  projectId: string;
  gramProject: string;
  accessKey: string;
}): readonly string[] {
  return [
    ...PROJECT_RESOURCE_INDEX_QUERY_PREFIX,
    parts.userId,
    parts.organizationId,
    parts.projectId,
    parts.gramProject,
    PROJECT_RESOURCE_INDEX_SCHEMA_VERSION,
    parts.accessKey,
  ] as const;
}

/** Loads one complete index per access boundary; query text never reaches the network. */
export function useProjectResourceIndexCandidates({
  enabled,
  accessKey,
}: {
  enabled: boolean;
  accessKey: string;
}): ProjectResourceIndexCandidatesResult {
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const gramProject = useProjectSlugForRequests();
  const organization = useOrganization();
  const project = useProject();
  const session = useSession();
  const requestCount = useRef(0);
  const requestCountAtOpen = useRef(0);
  const wasEnabled = useRef(false);
  if (enabled && !wasEnabled.current) {
    requestCountAtOpen.current = requestCount.current;
  }
  wasEnabled.current = enabled;
  const queryKey = useMemo(
    () =>
      projectResourceIndexQueryKey({
        userId: session.user.id,
        organizationId: organization.id,
        projectId: project.id,
        gramProject,
        accessKey,
      }),
    [accessKey, gramProject, organization.id, project.id, session.user.id],
  );
  const result = useQuery({
    queryKey,
    queryFn: ({ signal }) => {
      requestCount.current += 1;
      return client.projectResourceIndex.get({ gramProject }, undefined, {
        signal,
      });
    },
    enabled,
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    retry: false,
    refetchOnWindowFocus: false,
    throwOnError: false,
  });
  const indexInFlight = enabled && result.fetchStatus === "fetching";
  const indexed = useMemo(() => {
    if (
      !result.data ||
      result.data.schemaVersion !== PROJECT_RESOURCE_INDEX_SCHEMA_VERSION
    )
      return null;
    const startedAt = performance.now();
    const candidates = mapResourceIndexItems(
      result.data.items,
      routes,
      navigate,
    );
    const metadata = knownKinds(result.data.kinds);
    const fallbackKinds = new Set<MigratedResourceKind>();
    for (const kind of MIGRATED_RESOURCE_KINDS) {
      const value = metadata.get(kind);
      if (!value?.complete || value.overflow) fallbackKinds.add(kind);
    }
    return {
      candidates,
      fallbackKinds,
      mappingDurationMs: performance.now() - startedAt,
      bytes: estimatePayloadBytes(result.data.items),
    };
  }, [navigate, result.data, routes]);
  if (indexErrorRequiresLegacyFallback(enabled, indexInFlight, result.isError))
    return {
      candidates: EMPTY_CANDIDATES,
      fallbackKinds: ALL_FALLBACK_KINDS,
      metrics: {
        ...EMPTY_METRICS,
        state: "fallback",
        indexRequestCount: requestCount.current,
      },
    };
  if (indexed)
    return {
      candidates: indexed.candidates,
      fallbackKinds: indexed.fallbackKinds,
      metrics: {
        state: indexed.fallbackKinds.size > 0 ? "fallback" : "ready",
        indexRequestCount: requestCount.current,
        indexInFlight,
        cacheHit: requestCount.current === requestCountAtOpen.current,
        itemCount: indexed.candidates.length,
        complete: indexed.fallbackKinds.size === 0,
        incompleteKindCount: indexed.fallbackKinds.size,
        payloadByteBucket: payloadBucket(indexed.bytes),
        mappingDurationMs: indexed.mappingDurationMs,
      },
    };
  return {
    candidates: EMPTY_CANDIDATES,
    fallbackKinds:
      enabled && indexInFlight ? EMPTY_FALLBACK_KINDS : ALL_FALLBACK_KINDS,
    metrics: enabled
      ? {
          ...EMPTY_METRICS,
          state: "loading",
          indexRequestCount: requestCount.current,
          indexInFlight,
        }
      : EMPTY_METRICS,
  };
}
