import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { mcpProxyUrl, useMcpUrl } from "@/hooks/useToolsetUrl";
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
import { useToolset } from "@gram/client/react-query/toolset.js";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query/index.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { AlertCircle, HistoryIcon, ShieldAlert } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { GramThreadWelcome } from "./PlaygroundElementsOverrides";
import {
  getExternalMcpOAuthConfig,
  useExternalMcpOAuthStatus,
} from "./PlaygroundAuth";

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

  // Get MCP URL from toolset (uses custom domain if configured)
  const { url: mcpUrl } = useMcpUrl(toolset);

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
  const missingAuthCount = useMissingRequiredEnvVars(
    toolset as Toolset | undefined,
    environments,
    environmentSlug ?? defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Check if this toolset requires external MCP OAuth
  const mcpOAuthConfig = useMemo(
    () =>
      toolset?.tools ? getExternalMcpOAuthConfig(toolset.tools) : undefined,
    [toolset?.tools],
  );

  const { data: oauthStatus, isLoading: oauthStatusLoading } =
    useExternalMcpOAuthStatus(toolset?.id, {
      slug: mcpOAuthConfig?.slug,
      enabled: !!mcpOAuthConfig,
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

  // Block rendering if OAuth is required but user is not authenticated
  if (
    mcpOAuthConfig &&
    !oauthStatusLoading &&
    oauthStatus?.status !== "authenticated"
  ) {
    return (
      <OAuthRequiredNotice
        providerName={mcpOAuthConfig.name || mcpOAuthConfig.slug}
      />
    );
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
            ...userProvidedHeaders,
          },
        },
        history: {
          enabled: true,
          showThreadList: true,
          initialThreadId,
        },
        mcp: mcpProxyUrl(mcpUrl),
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
          toolMentions: true,
        },
        theme: {
          colorScheme: resolvedTheme === "dark" ? "dark" : "light",
          density: "normal",
          radius: "soft",
        },
        components: {
          ThreadWelcome: GramThreadWelcome,
        },
      }}
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
              className="w-72 p-0 max-h-96 overflow-y-scroll"
            >
              <ChatHistory className="h-full max-h-96 overflow-y-auto" />
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
        <div className="h-full overflow-hidden">
          <Chat />
        </div>
      </div>
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
    <div className="flex items-center gap-2 px-4 py-2.5 bg-warning/15 border-b border-warning/30 text-sm font-medium text-warning-foreground">
      <AlertCircle className="size-4 shrink-0" />
      <span>
        {missingCount} authentication{" "}
        {missingCount === 1 ? "variable" : "variables"} not configured.{" "}
        <routes.mcp.details.Link
          params={[toolsetSlug]}
          hash="authentication"
          className="underline hover:text-foreground font-medium"
        >
          Configure now
        </routes.mcp.details.Link>
      </span>
    </div>
  );
}

function OAuthRequiredNotice({ providerName }: { providerName: string }) {
  return (
    <div className="h-full flex items-center justify-center">
      <div className="flex flex-col items-center gap-3 text-center max-w-md px-4">
        <div className="rounded-full bg-warning/15 p-3">
          <ShieldAlert className="size-6 text-warning" />
        </div>
        <Type className="font-medium">OAuth Connection Required</Type>
        <Type muted className="text-sm">
          This MCP server requires authentication with{" "}
          <span className="font-medium text-foreground">{providerName}</span>.
          Use the <span className="font-medium text-foreground">Connect</span>{" "}
          button in the Authentication section of the sidebar to authorize
          access.
        </Type>
      </div>
    </div>
  );
}
