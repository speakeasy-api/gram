import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  Chat,
  ChatHistory,
  GramElementsProvider,
  type Model,
} from "@gram-ai/elements";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query/index.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, HistoryIcon, ShieldAlert } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  getToolsetOAuthMode,
  useExternalMcpOAuthStatus,
} from "./playground-auth-utils";
import { GramThreadWelcome } from "./PlaygroundElementsOverrides";
import {
  PlaygroundMcpAppsProvider,
  PlaygroundMcpToolFallback,
} from "./PlaygroundMcpApps";

interface PlaygroundElementsProps {
  toolsetSlug: string | null;
  environmentSlug: string | null;
  model: string;
  /** Additional action buttons to render alongside the share button */
  additionalActions?: React.ReactNode;
  /** Slug of the playground environment for user-provided variables */
  playgroundEnvironmentSlug?: string;
}

export function PlaygroundElements({
  toolsetSlug,
  environmentSlug,
  model,
  additionalActions,
  playgroundEnvironmentSlug,
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
  const { data: toolset } = useToolset(toolsetSlug ?? undefined);

  // Always use the platform domain for the playground to avoid CSP issues
  const mcpUrl = useInternalMcpUrl(toolset);

  // Get environments and MCP metadata for auth status check
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolsetSlug ?? "" },
    undefined,
    { throwOnError: false, retry: false, enabled: !!toolsetSlug },
  );
  const mcpMetadata = mcpMetadataData?.metadata;
  const defaultEnvironmentSlug =
    environments.find((env) => env.id === mcpMetadata?.defaultEnvironmentId)
      ?.slug ?? "default";

  // ToolsetEntry from useListToolsets is structurally compatible with Toolset
  // for the fields useMissingRequiredEnvVars accesses (same pattern as Playground.tsx)
  //
  // Intentionally do NOT pass playgroundEnvironmentSlug here. The playground
  // environment only stores user-provided entries, so system variables would
  // always appear missing if we pointed the hook at it. User-provided vars
  // are already treated as always-configured by useMissingRequiredEnvVars
  // regardless of the environment, so using the default env here is correct
  // for both kinds of variables.
  const missingAuthCount = useMissingRequiredEnvVars(
    toolset as Toolset | undefined,
    environments,
    environmentSlug ?? defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Check if this toolset requires OAuth at the toolset level
  const oauthMode = useMemo(
    () => (toolset ? getToolsetOAuthMode(toolset) : "none"),
    [toolset],
  );
  const hasOAuth = oauthMode !== "none";

  const { data: oauthStatus, isLoading: oauthStatusLoading } =
    useExternalMcpOAuthStatus(toolset?.id, {
      enabled: hasOAuth,
    });

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

  const mcpAppSessionQuery = useQuery({
    queryKey: [
      "playground-mcp-app-session",
      project.id,
      toolsetSlug,
      environmentSlug,
      session.user.id,
    ],
    queryFn: getSession,
    enabled: !!mcpUrl && !!toolsetSlug,
    staleTime: 1000 * 60 * 30,
  });

  const effectiveEnvironmentSlug = playgroundEnvironmentSlug ?? environmentSlug;

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

  // Don't render until we have a valid MCP URL
  if (!mcpUrl || !toolsetSlug) {
    return (
      <div className="flex h-full items-center justify-center">
        <Type muted>Select an MCP server to start chatting</Type>
      </div>
    );
  }

  // Block rendering if OAuth is required but user is not authenticated
  if (
    hasOAuth &&
    !oauthStatusLoading &&
    oauthStatus?.status !== "authenticated"
  ) {
    const providerName =
      toolset?.oauthProxyServer?.oauthProxyProviders?.[0]?.slug ??
      toolset?.externalOauthServer?.slug ??
      toolset?.name ??
      "provider";
    return <OAuthRequiredNotice providerName={providerName} />;
  }

  return (
    <GramElementsProvider
      config={{
        projectSlug: project.slug,
        api: {
          url: getServerURL(),
          session: getSession,
          headers: {
            "X-Gram-Source": "playground",
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
          {missingAuthCount > 0 && toolsetSlug && (
            <AuthWarningBanner
              missingCount={missingAuthCount}
              toolsetSlug={toolsetSlug}
            />
          )}
          <div className="min-h-0 flex-1 overflow-hidden">
            <Chat />
          </div>
        </div>
      </PlaygroundMcpAppsProvider>
    </GramElementsProvider>
  );
}

function AuthWarningBanner({
  missingCount,
  toolsetSlug,
}: {
  missingCount: number;
  toolsetSlug: string;
}) {
  const routes = useRoutes();

  return (
    <div className="bg-warning/15 border-warning/30 text-warning-foreground flex items-center gap-2 border-b px-4 py-2.5 text-sm font-medium">
      <AlertCircle className="size-4 shrink-0" />
      <span>
        {missingCount} authentication{" "}
        {missingCount === 1 ? "variable" : "variables"} not configured.{" "}
        <routes.mcp.details.Link
          params={[toolsetSlug]}
          hash="authentication"
          className="hover:text-foreground font-medium underline"
        >
          Configure now
        </routes.mcp.details.Link>
      </span>
    </div>
  );
}

function OAuthRequiredNotice({ providerName }: { providerName: string }) {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="flex max-w-md flex-col items-center gap-3 px-4 text-center">
        <div className="bg-warning/15 rounded-full p-3">
          <ShieldAlert className="text-warning size-6" />
        </div>
        <Type className="font-medium">OAuth Connection Required</Type>
        <Type muted className="text-sm">
          This MCP server requires authentication with{" "}
          <span className="text-foreground font-medium">{providerName}</span>.
          Use the <span className="text-foreground font-medium">Connect</span>{" "}
          button in the Authentication section of the sidebar to authorize
          access.
        </Type>
      </div>
    </div>
  );
}
