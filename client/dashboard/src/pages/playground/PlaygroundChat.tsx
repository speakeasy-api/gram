import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useProject, useSession } from "@/contexts/Auth";
import type { Toolset } from "@/lib/toolTypes";
import { getPlaygroundMcpBaseURL } from "@/lib/utils";
import {
  Chat,
  ChatHistory,
  GramElementsProvider,
  type Model,
} from "@gram-ai/elements";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { HistoryIcon } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  PlaygroundMcpAppsProvider,
  PlaygroundMcpToolFallback,
} from "./PlaygroundMcpApps";
import { GramThreadWelcome } from "./PlaygroundElementsOverrides";

interface PlaygroundChatProps {
  /** Resolved MCP server URL (Gram origin `/mcp/<slug>`). */
  mcpUrl: string;
  /**
   * User-session JWT forwarded as `Authorization: Bearer` so the runtime
   * gateway resolves the dashboard user's stored upstream credentials.
   */
  gatewayToken?: string;
  model: string;
  environmentSlug: string | null;
  /** Slug of the playground environment for user-provided variables. */
  playgroundEnvironmentSlug?: string;
  /**
   * The backing toolset, used only to register MCP-App tool/resource UIs.
   * Undefined for remote-MCP-backed servers, which carry no Gram-side apps.
   */
  toolset?: Toolset;
  /** Action buttons rendered in the chat header (share, logs, …). */
  additionalActions?: React.ReactNode;
  /** Optional warning banner rendered above the chat (e.g. missing auth). */
  banner?: React.ReactNode;
}

/**
 * The shared playground chat surface: the `@gram-ai/elements` provider, the
 * MCP-Apps provider, the chat-history popover, and the chat itself. Both the
 * toolset-backed and remote-MCP-backed variants resolve their own MCP URL and
 * gateway token, then render through here so the chat looks and behaves
 * identically regardless of backend.
 */
export function PlaygroundChat({
  mcpUrl,
  gatewayToken,
  model,
  environmentSlug,
  playgroundEnvironmentSlug,
  toolset,
  additionalActions,
  banner,
}: PlaygroundChatProps): JSX.Element {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();
  const { theme: resolvedTheme } = useMoonshineConfig();
  const [historyOpen, setHistoryOpen] = useState(false);
  const [searchParams] = useSearchParams();

  const initialThreadId = searchParams.get("threadId") ?? undefined;

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
      toast.error("Failed to create session. Please try again.");
      throw error;
    }
  }, [
    createSessionMutation,
    session.session,
    session.user.id,
    project.id,
    project.slug,
  ]);

  const effectiveEnvironmentSlug = playgroundEnvironmentSlug ?? environmentSlug;

  const mcpAppSessionQuery = useQuery({
    queryKey: [
      "playground-mcp-app-session",
      project.id,
      mcpUrl,
      effectiveEnvironmentSlug,
      session.user.id,
    ],
    queryFn: getSession,
    enabled: !!mcpUrl,
    staleTime: 1000 * 60 * 30,
  });

  const mcpAppHeaders = useMemo(() => {
    if (!mcpAppSessionQuery.data) {
      return null;
    }

    return {
      "Gram-Chat-Session": mcpAppSessionQuery.data,
      "Gram-Project": project.slug,
      ...(effectiveEnvironmentSlug
        ? { "Gram-Environment": effectiveEnvironmentSlug }
        : {}),
    };
  }, [effectiveEnvironmentSlug, mcpAppSessionQuery.data, project.slug]);

  return (
    <GramElementsProvider
      config={{
        projectSlug: project.slug,
        api: {
          url: getPlaygroundMcpBaseURL(),
          session: getSession,
          headers: {
            "X-Gram-Source": "playground",
            // Forwarded to /mcp/{slug} so the issuer-gated runtime gate can
            // resolve the dashboard user's stored upstream credentials.
            ...(gatewayToken
              ? { Authorization: `Bearer ${gatewayToken}` }
              : {}),
          },
        },
        history: {
          enabled: true,
          showThreadList: true,
          initialThreadId,
        },
        mcp: mcpUrl,
        gramEnvironment:
          playgroundEnvironmentSlug ?? environmentSlug ?? undefined,
        variant: "standalone",
        model: {
          defaultModel: model as Model,
          showModelPicker: false,
        },
        welcome: {
          title: "Test Your MCP Server",
          subtitle:
            "This chat has access to the selected MCP server. Use it to test your tools.",
          suggestions: [
            {
              title: "Explore tools",
              label: "See what's available",
              prompt: "What tools does this server have?",
            },
          ],
        },
        composer: {
          placeholder: "Send a message...",
          toolMentions: true,
        },
        theme: {
          colorScheme: resolvedTheme === "dark" ? "dark" : "light",
          density: "normal",
          radius: "soft",
        },
        components: {
          ToolFallback: PlaygroundMcpToolFallback,
          ThreadWelcome: GramThreadWelcome,
        },
      }}
    >
      <PlaygroundMcpAppsProvider
        headers={mcpAppHeaders}
        mcpUrl={mcpUrl}
        theme={resolvedTheme === "dark" ? "dark" : "light"}
        toolset={toolset}
      >
        <div className="flex h-full min-h-0 flex-col">
          <div className="border-b-border flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3">
            <Popover open={historyOpen} onOpenChange={setHistoryOpen}>
              <PopoverTrigger asChild>
                <Button size="sm" variant="ghost">
                  <HistoryIcon className="mr-2 size-4" />
                  Chat History
                </Button>
              </PopoverTrigger>
              <PopoverContent
                align="start"
                className="max-h-96 min-h-fit w-72 overflow-y-scroll p-0"
              >
                <ChatHistory className="h-full max-h-96 min-h-fit overflow-y-auto" />
              </PopoverContent>
            </Popover>
            <div className="flex items-center gap-2">{additionalActions}</div>
          </div>
          {banner}
          <div className="min-h-0 flex-1 overflow-hidden">
            <Chat />
          </div>
        </div>
      </PlaygroundMcpAppsProvider>
    </GramElementsProvider>
  );
}
