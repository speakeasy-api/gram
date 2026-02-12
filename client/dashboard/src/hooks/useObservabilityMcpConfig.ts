import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import type { ElementsConfig } from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext, useListToolsets } from "@gram/client/react-query";
import { useCallback, useMemo } from "react";

type ToolFilter = (params: { toolName: string }) => boolean;

interface ObservabilityMcpConfigOptions {
  toolsToInclude: ToolFilter;
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
  const isLocal = process.env.NODE_ENV === "development";
  const { session } = useSession();

  // For local development, look up the gram-seed toolset in the ecommerce-api project
  const { data: toolsets } = useListToolsets(
    {
      gramProject: "ecommerce-api",
    },
    undefined,
    {
      enabled: isLocal,
      headers: {
        "gram-project": "ecommerce-api",
      },
    },
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

  const gramToolset = useMemo(() => {
    return toolsets?.toolsets.find((toolset) => toolset.slug === "gram-seed");
  }, [toolsets]);

  return useMemo(() => {
    if (!projectSlug) {
      throw new Error("No project slug found.");
    }

    const baseConfig: Omit<ElementsConfig, "variant" | "welcome" | "theme"> = {
      projectSlug,
      tools: {
        toolsToInclude,
      },
      api: {
        url: getServerURL(),
        sessionFn: getSession,
      },
      environment: {
        GRAM_SERVER_URL: getServerURL(),
        GRAM_SESSION_HEADER_GRAM_SESSION: session,
        GRAM_APIKEY_HEADER_GRAM_KEY: "", // This must be set or else the tool call will fail
        GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT: projectSlug,
      },
    };

    if (isLocal) {
      if (toolsets && !gramToolset) {
        console.warn("No gram-seed toolset found--have you run mise seed?");
        return baseConfig;
      }

      return {
        ...baseConfig,
        ...(gramToolset && {
          mcp: `${getServerURL()}/mcp/${gramToolset?.mcpSlug}`,
        }),
      };
    }

    const mcpUrl = getServerURL().includes("app.getgram.ai")
      ? "https://app.getgram.ai/mcp/speakeasy-team-gram"
      : "https://dev.getgram.ai/mcp/speakeasy-team-gram";

    return {
      ...baseConfig,
      mcp: mcpUrl,
    };
  }, [
    toolsToInclude,
    getSession,
    session,
    projectSlug,
    isLocal,
    toolsets,
    gramToolset,
  ]);
}
