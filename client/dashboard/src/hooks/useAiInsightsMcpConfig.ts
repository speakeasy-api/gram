import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import type { ElementsConfig } from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext } from "@gram/client/react-query";
import { useCallback, useMemo } from "react";

/**
 * Hook to generate MCP configuration for Gram's built-in `ai-insights` MCP
 * server. Unlike `useObservabilityMcpConfig`, this one always points at the
 * same origin as the dashboard — the MCP lives at `/mcp/ai-insights` on the
 * Gram server so there is no per-environment URL to override.
 */
export function useAiInsightsMcpConfig(): Omit<
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

  return useMemo(() => {
    if (!projectSlug) {
      throw new Error("No project slug found.");
    }

    const serverURL = getServerURL();
    const mcpUrl = `${serverURL}/mcp/ai-insights`;

    return {
      projectSlug,
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
      mcp: mcpUrl,
    };
  }, [getSession, session, projectSlug]);
}
