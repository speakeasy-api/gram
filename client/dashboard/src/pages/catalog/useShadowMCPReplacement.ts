import { useOrganization, useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { CatalogServer } from "@/pages/catalog/hooks";
import { useFindShadowMCPInventoryServers } from "@gram/client/react-query/findShadowMCPInventoryServers.js";
import { useMemo, useState } from "react";
import { filterToHttpRemotes } from "./remotes";

/** A Shadow MCP server in use that a catalog endpoint can replace. */
export interface ShadowMCPReplacement {
  /** The project the Shadow MCP server was seen in. */
  projectId: string;
  /** The catalog endpoint URL that matches, exactly as the install uses it. */
  remoteUrl: string;
  /** The Shadow MCP server's URL, which the official-server link is keyed by. */
  shadowServerUrl: string;
  /** What the Shadow MCP page calls the server. */
  shadowName: string;
  /** The Shadow MCP server's page. */
  shadowServerSlug: string;
  /** Distinct people who reached the server directly. */
  userCount: number;
}

export interface ShadowMCPReplacementState {
  /** Shadow MCP servers in use that share an upstream with the endpoints
   *  being added and have no official server yet. */
  candidates: ShadowMCPReplacement[];
  /** The candidates the admin chose to replace: every candidate unless
   *  switched off. */
  selected: ShadowMCPReplacement[];
  isSelected: (candidate: ShadowMCPReplacement) => boolean;
  setSelected: (candidate: ShadowMCPReplacement, selected: boolean) => void;
  /** True until the lookup answers, so the dialog does not skip past the
   *  question before it can be asked. */
  isLoading: boolean;
  /** Forget the admin's choices, for a dialog that closed. */
  reset: () => void;
}

/**
 * Finds the Shadow MCP servers in use that the catalog servers being added
 * would replace, and holds the admin's choice. Replacing is on by default.
 * Only organization admins manage Shadow MCP servers, so for anyone else, and
 * when the lookup fails, there is nothing to offer and the install goes on as
 * a plain add.
 */
export function useShadowMCPReplacement({
  servers,
  projectSlug,
  enabled,
}: {
  servers: CatalogServer[];
  projectSlug?: string;
  enabled: boolean;
}): ShadowMCPReplacementState {
  const organization = useOrganization();
  const currentProject = useProject();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const projectId = projectSlug
    ? organization.projects.find((project) => project.slug === projectSlug)?.id
    : currentProject.id;
  const isOrgAdmin = !rbacLoading && hasScope("org:admin", organization.id);

  const remoteUrls = useMemo(
    () => [
      ...new Set(
        servers.flatMap(
          (server) =>
            filterToHttpRemotes(server).remotes?.map((remote) => remote.url) ??
            [],
        ),
      ),
    ],
    [servers],
  );

  const queryEnabled =
    enabled && isOrgAdmin && !!projectId && remoteUrls.length > 0;
  const query = useFindShadowMCPInventoryServers(
    { projectId: projectId ?? "", serverUrls: remoteUrls },
    undefined,
    { enabled: queryEnabled, retry: false, throwOnError: false },
  );

  const candidates = useMemo<ShadowMCPReplacement[]>(() => {
    if (!queryEnabled) return [];
    return (query.data?.matches ?? []).flatMap((match) => {
      // A Shadow MCP server that already has an official version, switched
      // on or off, is the admin's settled choice: adding another server must
      // not take it over.
      if (match.server.officialServer) return [];
      return [
        {
          projectId: projectId ?? "",
          remoteUrl: match.serverUrl,
          shadowServerUrl: match.server.canonicalServerUrl,
          shadowName: match.server.serverName || match.server.urlHost,
          shadowServerSlug: match.server.serverSlug,
          userCount: match.server.userCount,
        },
      ];
    });
  }, [projectId, query.data, queryEnabled]);

  // Switched-off candidates, by catalog endpoint URL, so a refetch keeps the
  // admin's choice.
  const [declined, setDeclined] = useState<ReadonlySet<string>>(new Set());
  const isSelected = (candidate: ShadowMCPReplacement) =>
    !declined.has(candidate.remoteUrl);
  const setSelected = (candidate: ShadowMCPReplacement, selected: boolean) =>
    setDeclined((previous) => {
      const next = new Set(previous);
      if (selected) next.delete(candidate.remoteUrl);
      if (!selected) next.add(candidate.remoteUrl);
      return next;
    });

  const selected = useMemo(
    () => candidates.filter((candidate) => !declined.has(candidate.remoteUrl)),
    [candidates, declined],
  );

  return {
    candidates,
    selected,
    isSelected,
    setSelected,
    isLoading: queryEnabled && query.isLoading,
    reset: () => setDeclined(new Set()),
  };
}
