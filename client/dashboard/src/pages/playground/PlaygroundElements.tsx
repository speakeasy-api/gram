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
  GramThreadWelcome,
  GramUserMessage,
} from "./PlaygroundElementsOverrides";
import { useEnvironment } from "../environments/Environment";
import { getAuthStatus } from "./PlaygroundAuth";
import { useTheme } from "next-themes";
import { toast } from "sonner";

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
          UserMessage: GramUserMessage,
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
          <div className="flex items-center justify-end gap-2 py-3 shrink-0">
            {additionalActions}
          </div>
          <div className="flex-1 min-h-0 overflow-hidden bg-surface-primary [&_.aui-thread-root]:bg-transparent [&_.aui-composer-root]:rounded [&_.aui-composer-wrapper]:bg-transparent rounded-br-xl">
            <Chat />
          </div>
        </div>
      </PlaygroundAuthWarningContext.Provider>
    </GramElementsProvider>
  );
}
