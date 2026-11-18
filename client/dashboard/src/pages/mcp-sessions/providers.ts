import { useProject } from "@/contexts/Auth";
import { useEffectiveUserSessionIssuers } from "@/hooks/useEffectiveUserSessionIssuers";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

/** The tabs a provider's page shows, in order, by URL segment. */
export const PROVIDER_TABS = [
  "connections",
  "mcp-servers",
  "settings",
] as const;
export type ProviderTab = (typeof PROVIDER_TABS)[number];

/** With connection health: Overview first, and Connection Logs beside Connections. */
export const HEALTH_PROVIDER_SEGMENTS = [
  "overview",
  "connections",
  "connection-logs",
  "mcp-servers",
  "settings",
] as const;
export type HealthProviderSegment = (typeof HEALTH_PROVIDER_SEGMENTS)[number];

/** The slug the organization's own provider is created with. */
const DEFAULT_PROVIDER_SLUG = "default";

/**
 * The MCP Authentication Providers this project's servers can use: the
 * organization-wide ones first, then the project's own, each by name. Read
 * through the project, so anyone who can see its sessions can pick one.
 */
export function useProjectProviders(): {
  providers: UserSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
} {
  const project = useProject();
  const { issuers, isLoading, isError } = useEffectiveUserSessionIssuers({
    projectSlug: project.slug,
  });
  return { providers: issuers, isLoading, isError };
}

/** The provider this project's MCP Sessions opened on last, by id. */
export function useLastProvider(): readonly [
  string | null,
  (issuerId: string) => void,
] {
  const project = useProject();
  return useLocalStorageState<string | null>(
    `mcp-sessions:last-provider:${project.id}`,
    null,
  );
}

/**
 * Which provider MCP Sessions opens on: the one asked for, else the one
 * picked last, else the organization's default, else the first listed.
 */
export function initialProvider(
  providers: UserSessionIssuer[],
  { requested, last }: { requested?: string | null; last?: string | null },
): UserSessionIssuer | undefined {
  const byId = (id: string | null | undefined) =>
    id ? providers.find((provider) => provider.id === id) : undefined;
  return (
    byId(requested) ??
    byId(last) ??
    providers.find(
      (provider) =>
        provider.projectId === "" && provider.slug === DEFAULT_PROVIDER_SLUG,
    ) ??
    providers[0]
  );
}

/** Where the provider's scope reads, beside its name. */
export function providerScopeLabel(
  provider: Pick<UserSessionIssuer, "projectId">,
): string {
  return provider.projectId === "" ? "Organization-wide" : "This project";
}

/** The client access mode as Settings words it. */
export function clientAccessLabel(mode: string): string {
  if (mode === "presets") return "Known clients";
  // "reporting" is a legacy value that admits what "open" admits.
  if (mode === "open" || mode === "reporting") return "Any client";
  return "Off";
}
