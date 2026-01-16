import { Chat, GramElementsProvider, type Model } from "@gram-ai/elements";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useListToolsets } from "@gram/client/react-query/index.js";
import { useTheme } from "next-themes";
import { createContext, useCallback, useContext } from "react";
import { toast } from "sonner";
import { useEnvironment } from "../environments/Environment";
import { useMcpUrl } from "../mcp/MCPDetails";
import { getAuthStatus } from "./PlaygroundAuth";
import {
  GramThreadWelcome,
  GramUserMessage,
} from "./PlaygroundElementsOverrides";
import "@gram-ai/elements/elements.css";

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
  /** Additional action buttons to render alongside the share button */
  additionalActions?: React.ReactNode;
}

export function PlaygroundElements({
  toolsetSlug,
  environmentSlug,
  model,
  additionalActions,
}: PlaygroundElementsProps) {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();
  const { resolvedTheme } = useTheme();

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
    try {
      const result = await createSessionMutation.mutateAsync({
        request: {
          gramProject: project.id,
          createRequestBody: {
            embedOrigin: window.location.origin,
            expiresAfter: 3600,
            userIdentifier: session.user.id,
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
    } catch (error) {
      toast.error("Failed to create chat session. Please try again.");
      throw error;
    }
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
        history: {
          enabled: true,
          showThreadList: true,
        },
        mcp: mcpUrl,
        gramEnvironment: environmentSlug ?? undefined,
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
          colorScheme: resolvedTheme === "dark" ? "dark" : "light",
          density: "normal",
          radius: "soft",
        },
        components: {
          ThreadWelcome: GramThreadWelcome,
          // UserMessage: GramUserMessage,
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
        <div className="h-full flex flex-col min-h-0">
          <div className="flex items-center justify-end gap-2 py-3 shrink-0 border-b-border border-b">
            {additionalActions}
          </div>
          <div className="h-full overflow-hidden">
            <Chat />
          </div>
        </div>
      </PlaygroundAuthWarningContext.Provider>
    </GramElementsProvider>
  );
}
