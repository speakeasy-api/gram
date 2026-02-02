import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import {
  Chat,
  ChatHistory,
  GramElementsProvider,
  type Model,
} from "@gram-ai/elements";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useToolset } from "@gram/client/react-query/toolset.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { HistoryIcon } from "lucide-react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { useEnvironment } from "../environments/Environment";
import { getAuthStatus } from "./PlaygroundAuth";
import {
  GramComposer,
  GramThreadWelcome,
  PlaygroundAuthWarningContext,
} from "./PlaygroundElementsOverrides";

interface PlaygroundElementsProps {
  toolsetSlug: string | null;
  environmentSlug: string | null;
  model: string;
  /** Additional action buttons to render alongside the share button */
  additionalActions?: React.ReactNode;
  /** User-provided auth headers for user-provided variables */
  userProvidedHeaders?: Record<string, string>;
}

export function PlaygroundElements({
  toolsetSlug,
  environmentSlug,
  model,
  additionalActions,
  userProvidedHeaders = {},
}: PlaygroundElementsProps) {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();
  const { theme: resolvedTheme } = useMoonshineConfig();
  const [historyOpen, setHistoryOpen] = useState(false);
  const [searchParams] = useSearchParams();

  // Get threadId from URL for shared chat links
  const initialThreadId = searchParams.get("threadId") ?? undefined;

  // Get toolset data to construct MCP URL
  const { data: toolset } = useToolset(
    { slug: toolsetSlug ?? "" },
    {},
    { enabled: !!toolsetSlug },
  );

  // Get MCP URL from toolset (always uses Gram domain, not custom domains)
  const mcpUrl = useInternalMcpUrl(toolset);

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
  }, [
    createSessionMutation,
    session.session,
    session.user.id,
    project.id,
    project.slug,
  ]);

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
          headers: {
            "X-Gram-Source": "playground",
            ...userProvidedHeaders,
          },
        },
        history: {
          enabled: true,
          showThreadList: true,
          initialThreadId,
        },
        mcp: mcpUrl,
        gramEnvironment: environmentSlug ?? undefined,
        environment: {
          ...userProvidedHeaders,
        },
        variant: "standalone",
        model: {
          defaultModel: model as Model,
          showModelPicker: false,
        },
        welcome: {
          title: "Test Your MCP Server",
          subtitle:
            "This chat has access to the selected MCP server. Use it to test your tools.",
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
          Composer: GramComposer,
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
          <div className="flex items-center justify-between gap-2 py-3 shrink-0 border-b-border border-b px-4">
            <Popover open={historyOpen} onOpenChange={setHistoryOpen}>
              <PopoverTrigger asChild>
                <Button size="sm" variant="ghost">
                  <HistoryIcon className="size-4 mr-2" />
                  Chat History
                </Button>
              </PopoverTrigger>
              <PopoverContent
                align="start"
                className="w-72 p-0 max-h-96 overflow-hidden"
              >
                <ChatHistory className="h-full max-h-96 overflow-y-auto" />
              </PopoverContent>
            </Popover>
            <div className="flex items-center gap-2">{additionalActions}</div>
          </div>
          <div className="h-full overflow-hidden">
            <Chat />
          </div>
        </div>
      </PlaygroundAuthWarningContext.Provider>
    </GramElementsProvider>
  );
}
