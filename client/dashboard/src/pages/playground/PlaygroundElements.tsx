import { GramElementsProvider, Chat, type Model } from "@gram-ai/elements";
// Note: Not importing Elements CSS as it conflicts with dashboard's Tailwind styles
// The dashboard's Tailwind should provide necessary utility classes
import { createContext, useCallback, useContext } from "react";
import { useProject, useSession } from "@/contexts/Auth";
import { useMcpUrl } from "../mcp/MCPDetails";
import { useListToolsets } from "@gram/client/react-query/index.js";
import { Type } from "@/components/ui/type";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { getServerURL } from "@/lib/utils";
import {
  Composer,
  GramThreadWelcome,
  GramUserMessage,
} from "./PlaygroundElementsOverrides";
import { useEnvironment } from "../environments/Environment";
import { getAuthStatus } from "./PlaygroundAuth";

// Context for passing auth warning to the Composer component
type AuthWarningValue = { missingCount: number; toolsetSlug: string } | null;
export const PlaygroundAuthWarningContext =
  createContext<AuthWarningValue>(null);
export const usePlaygroundAuthWarning = () =>
  useContext(PlaygroundAuthWarningContext);

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

  // Get environment data for auth status check
  const environmentData = useEnvironment(environmentSlug ?? undefined);

  // Calculate auth status
  const authStatus =
    toolset && environmentData
      ? getAuthStatus(toolset, {
          entries: environmentData.entries?.map((e) => ({
            name: e.name,
            value: e.value,
          })),
        })
      : null;

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
        api: {
          url: getServerURL(),
          sessionFn: getSession,
        },
        mcp: mcpUrl,
        envSlug: environmentSlug ?? undefined,
        variant: "standalone",
        model: {
          defaultModel: model as Model,
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
        theme: {
          colorScheme: "light",
          density: "normal",
          radius: "soft",
        },
        components: {
          ThreadWelcome: GramThreadWelcome,
          UserMessage: GramUserMessage,
          Composer: Composer,
        },
      }}
    >
      <PlaygroundAuthWarningContext.Provider
        value={
          authStatus?.hasMissingAuth && toolsetSlug
            ? { missingCount: authStatus.missingCount, toolsetSlug }
            : null
        }
      >
        <div className="h-full bg-surface-primary [&_.aui-thread-root]:bg-transparent [&_.aui-composer-wrapper]:bg-transparent rounded-br-xl">
          <Chat />
        </div>
      </PlaygroundAuthWarningContext.Provider>
    </GramElementsProvider>
  );
}
