import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import type { ElementsConfig, ToolsFilter } from "@/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useCallback, useMemo } from "react";
import { observabilityMcpEntries } from "./observabilityMcpEntries";
import {
  isNoMcpAccessConfigured,
  settledListCount,
} from "./projectAssistantAccess";

interface ObservabilityMcpConfigOptions {
  toolsToInclude: ToolsFilter;
}

/**
 * Hook to generate MCP configuration for AI Insights copilot features.
 * Connects to project toolsets and directly hosted MCP servers, then
 * filters tools based on the provided filter function.
 */
export function useObservabilityMcpConfig({
  toolsToInclude,
}: ObservabilityMcpConfigOptions): Omit<
  ElementsConfig,
  "variant" | "welcome" | "theme"
> {
  const { projectSlug } = useSlugs();
  const client = useGramContext();
  const { session } = useSession();
  const enabled = Boolean(projectSlug);
  const request = projectSlug ? { gramProject: projectSlug } : undefined;
  const { data: toolsetsData, isLoading: toolsetsLoading } = useListToolsets(
    request,
    undefined,
    { enabled },
  );
  const { data: mcpServersData, isLoading: mcpServersLoading } = useMcpServers(
    request,
    undefined,
    { enabled },
  );
  const { data: endpointsData, isLoading: endpointsLoading } = useMcpEndpoints(
    request,
    undefined,
    { enabled },
  );

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

  // Undefined while listings are in flight; `[]` when settled empty so the
  // picker can tell "still loading" from "no servers attached".
  const mcps = useMemo(
    () =>
      projectSlug
        ? observabilityMcpEntries({
            projectSlug,
            serverURL: getServerURL(),
            toolsetsLoading,
            toolsets: toolsetsData?.toolsets,
            mcpServersLoading,
            mcpServers: mcpServersData?.mcpServers,
            endpointsLoading,
            endpoints: endpointsData?.mcpEndpoints,
          })
        : undefined,
    [
      projectSlug,
      toolsetsLoading,
      toolsetsData?.toolsets,
      mcpServersLoading,
      mcpServersData?.mcpServers,
      endpointsLoading,
      endpointsData?.mcpEndpoints,
    ],
  );

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
      ...(mcps !== undefined && { mcps }),
    };
  }, [toolsToInclude, getSession, session, projectSlug, mcps]);
}

/**
 * Whether the project has neither toolsets nor MCP servers.
 * Used to show a setup prompt in the AI Insights sidebar.
 */
export function useNoToolsetsConfigured(projectSlug?: string): boolean {
  const enabled = Boolean(projectSlug);
  const request = projectSlug ? { gramProject: projectSlug } : undefined;
  const {
    data: toolsetsData,
    isLoading: toolsetsLoading,
    isError: toolsetsFailed,
  } = useListToolsets(request, undefined, { enabled });
  const {
    data: mcpServersData,
    isLoading: mcpServersLoading,
    isError: mcpServersFailed,
  } = useMcpServers(request, undefined, { enabled });

  return isNoMcpAccessConfigured({
    projectSlug,
    toolsetsLoading,
    toolsetCount: settledListCount(toolsetsData, toolsetsData?.toolsets),
    mcpServersLoading,
    mcpServerCount: settledListCount(
      mcpServersData,
      mcpServersData?.mcpServers,
    ),
    toolsetsFailed,
    mcpServersFailed,
  });
}
