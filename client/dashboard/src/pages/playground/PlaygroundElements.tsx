import { GramElementsProvider, Chat, type Model } from "@gram-ai/elements";
// Note: Not importing Elements CSS as it conflicts with dashboard's Tailwind styles
// The dashboard's Tailwind should provide necessary utility classes
import { useCallback, useMemo } from "react";
import { useProject, useSession } from "@/contexts/Auth";
import { useMcpUrl } from "../mcp/MCPDetails";
import { useEnvironment } from "../environments/Environment";
import { useListToolsets } from "@gram/client/react-query/index.js";
import { Type } from "@/components/ui/type";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import { getServerURL } from "@/lib/utils";

interface PlaygroundElementsProps {
  toolsetSlug: string | null;
  environmentSlug: string | null;
  model: string;
}

export function PlaygroundElements({
  toolsetSlug,
  environmentSlug,
  model,
}: PlaygroundElementsProps) {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();

  // Get toolset data to construct MCP URL
  const { data: toolsetsData } = useListToolsets();
  const toolset = toolsetsData?.toolsets?.find((ts) => ts.slug === toolsetSlug);

  // Get MCP URL from toolset
  const { url: mcpUrl } = useMcpUrl(toolset);

  // Get environment entries for MCP headers
  const environmentData = useEnvironment(environmentSlug ?? undefined);
  console.log("Env data", environmentData);

  // Build environment headers from environment entries
  const environment = useMemo(() => {
    if (!environmentData?.entries) return {};

    return environmentData.entries.reduce(
      (acc, entry) => {
        if (entry.value) {
          acc[entry.name] = entry.value;
        }
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [environmentData?.entries]);

  // Create getSession function using SDK mutation with session auth
  const getSession = useCallback(async () => {
    const result = await createSessionMutation.mutateAsync({
      request: {
        createRequestBody: {
          embedOrigin: window.location.origin,
          expiresAfter: 3600,
        },
      },
      security: {
        option1: {
          sessionHeaderGramSession: session.session,
          projectSlugHeaderGramProject: project.slug,
        },
      },
    });
    return result.clientToken;
  }, [createSessionMutation, session.session, project.slug]);

  // Don't render until we have a valid MCP URL
  if (!mcpUrl || !toolsetSlug) {
    return (
      <div className="h-full flex items-center justify-center">
        <Type muted>Select a toolset to start chatting</Type>
      </div>
    );
  }

  return (
    <GramElementsProvider
      config={{
        projectSlug: project.slug,
        apiURL: getServerURL(),
        mcp: mcpUrl,
        environment,
        variant: "standalone",
        model: {
          // defaultModel: model as Model,
          showModelPicker: false,
        },
        welcome: {
          title: "Test Your Toolset",
          subtitle:
            "This chat has access to the selected toolset. Use it to test your tools.",
          suggestions: [],
        },
        composer: {
          placeholder: "Send a message...",
        },
      }}
      getSession={getSession}
    >
      <Chat />
    </GramElementsProvider>
  );
}
