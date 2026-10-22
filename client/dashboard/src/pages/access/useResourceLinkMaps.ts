import { useSession } from "@/contexts/Auth";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useMemo } from "react";

export type ResourceLinkMaps = {
  projectMap: Map<string, { slug: string; name: string }>;
  toolsetMap: Map<string, { slug: string; name: string; projectId: string }>;
  mcpServerMap: Map<
    string,
    { slug?: string; name?: string; projectId: string }
  >;
};

/**
 * The org-wide lookups `ResourceLink` resolves ids against. A failed read
 * leaves a map empty, which degrades a link to its id chip rather than taking
 * the page down.
 */
export function useResourceLinkMaps(enabled = true): ResourceLinkMaps {
  const { organization } = useSession();
  const { data: toolsetsData } = useListToolsetsForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const { data: mcpServersData } = useListMcpServersForOrg(
    undefined,
    undefined,
    { enabled, throwOnError: false },
  );
  const projectMap = useMemo(() => {
    const m = new Map<string, { slug: string; name: string }>();
    for (const p of organization.projects) {
      m.set(p.id, { slug: p.slug, name: p.name });
    }
    return m;
  }, [organization.projects]);
  const toolsetMap = useMemo(() => {
    const m = new Map<
      string,
      { slug: string; name: string; projectId: string }
    >();
    for (const t of toolsetsData?.toolsets ?? []) {
      m.set(t.id, { slug: t.slug, name: t.name, projectId: t.projectId });
    }
    return m;
  }, [toolsetsData]);
  // Resolves grants stored against mcp_servers row ids (remote/tunneled
  // backends). Toolset-backed grants store the toolset id and resolve via
  // toolsetMap instead.
  const mcpServerMap = useMemo(() => {
    const m = new Map<
      string,
      { slug?: string; name?: string; projectId: string }
    >();
    for (const s of mcpServersData?.mcpServers ?? []) {
      m.set(s.id, { slug: s.slug, name: s.name, projectId: s.projectId });
    }
    return m;
  }, [mcpServersData]);
  return { projectMap, toolsetMap, mcpServerMap };
}
