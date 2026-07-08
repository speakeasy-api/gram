import { useProject, useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { internalMcpUrl } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import type {
  ElementsConfig,
  MCPServerEntry,
  ToolsFilter,
} from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useCallback, useMemo } from "react";

interface ObservabilityMcpConfigOptions {
  toolsToInclude: ToolsFilter;
}

/**
 * Hook to generate MCP configuration for AI Insights copilot features.
 * Connects to all toolsets in the current project and filters tools
 * based on the provided filter function.
 */
export function useObservabilityMcpConfig({
  toolsToInclude,
}: ObservabilityMcpConfigOptions): Omit<
  ElementsConfig,
  "variant" | "welcome" | "theme"
> {
  const { projectSlug } = useSlugs();
  const project = useProject();
  const client = useGramContext();
  const { session } = useSession();
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();

  const getSession = useCallback(async (): Promise<string> => {
    const res = await chatSessionsCreate(
      client,
      {
        createRequestBody: {
          embedOrigin: window.location.origin,
        },
      },
      undefined,
      {
        headers: {
          "Gram-Project": projectSlug ?? "",
        },
      },
    );
    return res.value?.clientToken ?? "";
  }, [client, projectSlug]);

  // Build MCP server entries for all project toolsets
  const mcps = useMemo<MCPServerEntry[] | undefined>(() => {
    if (isLoadingToolsets || !toolsetsData?.toolsets?.length) {
      return undefined;
    }

    return toolsetsData.toolsets.map((toolset) => ({
      url: internalMcpUrl({ slug: project.slug }, toolset),
      name: toolset.slug,
      environment: toolset.defaultEnvironmentSlug,
    }));
  }, [toolsetsData?.toolsets, project.slug, isLoadingToolsets]);

  return useMemo(() => {
    if (!projectSlug) {
      throw new Error("No project slug found.");
    }

    const serverURL = getServerURL();

    return {
      projectSlug,
      tools: {
        toolsToInclude,
        // Collapse multi-tool groups to an "Executed N tools" summary by
        // default; the user expands a group to see the individual calls.
        expandToolGroupsByDefault: false,
      },
      api: {
        url: serverURL,
        session: getSession,
      },
      environment: {
        GRAM_SERVER_URL: serverURL,
        GRAM_SESSION_HEADER_GRAM_SESSION: session,
        GRAM_APIKEY_HEADER_GRAM_KEY: "",
        GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT: projectSlug,
      },
      ...(mcps && mcps.length > 0 && { mcps }),
    };
  }, [toolsToInclude, getSession, session, projectSlug, mcps]);
}

/**
 * Whether the project has no toolsets configured yet.
 * Used to show a setup prompt in the AI Insights sidebar.
 */
export function useNoToolsetsConfigured(projectSlug?: string): boolean {
  const { data: toolsetsData, isLoading } = useListToolsets(
    projectSlug ? { gramProject: projectSlug } : undefined,
    undefined,
    { enabled: Boolean(projectSlug) },
  );

  if (!projectSlug || isLoading) return false;
  return !toolsetsData?.toolsets?.length;
}
