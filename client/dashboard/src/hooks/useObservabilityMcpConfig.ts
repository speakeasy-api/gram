import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import type { ElementsConfig, ToolsFilter } from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext } from "@gram/client/react-query";
import { useCallback, useMemo } from "react";

interface ObservabilityMcpConfigOptions {
  toolsToInclude: ToolsFilter;
}

/**
 * Hook to generate MCP configuration for observability copilot features.
 * Filters tools based on the provided filter function.
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

  const getSession = useCallback(async (): Promise<string> => {
    const res = await chatSessionsCreate(
      client,
      {
        embedOrigin: window.location.origin,
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

  return useMemo(() => {
    if (!projectSlug) {
      throw new Error("No project slug found.");
    }

    const serverURL = getServerURL();

    const mcpUrl = serverURL.includes("app.getgram.ai")
      ? "https://app.getgram.ai/mcp/speakeasy-team-gram"
      : serverURL.includes("dev.getgram.ai")
        ? "https://dev.getgram.ai/mcp/speakeasy-team-gram"
        : import.meta.env.VITE_GRAM_OBSERVABILITY_MCP_URL || undefined;

    return {
      projectSlug,
      tools: {
        toolsToInclude,
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
      ...(mcpUrl && { mcp: mcpUrl }),
    };
  }, [toolsToInclude, getSession, session, projectSlug]);
}

/**
 * Whether the observability MCP URL is missing in local dev.
 * True when running in dev mode and `mise seed` hasn't been run
 * (VITE_GRAM_OBSERVABILITY_MCP_URL is not set).
 */
export const devObservabilityMcpMissing =
  import.meta.env.DEV && !import.meta.env.VITE_GRAM_OBSERVABILITY_MCP_URL;
