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
import { ExternalMCPToolDefinition } from "@gram/client/models/components";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useToolset } from "@gram/client/react-query/toolset.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { HistoryIcon } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
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

  const externalMcpOAuth = useExternalMcpOAuthToken({
    apiUrl: getServerURL(),
    toolsetSlug: toolsetSlug ?? "",
    sessionHeaders: {},
  });

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
          ...(externalMcpOAuth.accessToken
            ? { Authorization: `Bearer ${externalMcpOAuth.accessToken}` }
            : {}),
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

type OAuthStatus = "authenticated" | "unauthenticated" | "expired";

/**
 * Response from the backend OAuth token endpoint
 */
interface OAuthTokenResponse {
  status: OAuthStatus;
  access_token: string;
  token_type: string;
  expires_at?: string;
  scope?: string;
}

/**
 * Return type for the useOAuthToken hook
 */
export interface UseOAuthTokenResult {
  required: boolean;
  oauthStatus: OAuthStatus | null;
  /** The OAuth access token (only available when authenticated) */
  accessToken: string | null;
  /** Token type (e.g., 'Bearer') */
  tokenType: string | null;
  /** Whether the token is being fetched */
  isLoading: boolean;
  /** Error message if token fetch failed */
  error: string | null;
  /** Token expiration time */
  expiresAt: Date | null;
  /** OAuth scopes granted */
  scope: string | null;
  /** Refetch the token */
  refetch: () => Promise<void>;
}

/**
 * Hook to fetch the OAuth access token for authenticated users.
 *
 * This hook retrieves the decrypted OAuth access token from the backend,
 * which can be used for making authenticated requests to external APIs.
 *
 * **Note:** For MCP tool execution, you typically don't need this hook
 * as the backend automatically retrieves and uses the OAuth token.
 * This hook is useful when you need to make direct authenticated
 * requests from the frontend or display token information.
 *
 * @example
 * ```tsx
 * const { accessToken, isLoading, error } = useOAuthToken({
 *   apiUrl: 'https://app.getgram.ai',
 *   auth: config.api,
 *   sessionHeaders: { 'Gram-Chat-Session': sessionToken },
 * });
 *
 * if (accessToken) {
 *   // Use token for authenticated API calls
 *   fetch('https://api.example.com/data', {
 *     headers: { Authorization: `Bearer ${accessToken}` }
 *   });
 * }
 * ```
 */
const useExternalMcpOAuthToken = ({
  apiUrl,
  toolsetSlug,
  sessionHeaders,
}: {
  apiUrl: string;
  toolsetSlug: string;
  sessionHeaders: Record<string, string>;
}): UseOAuthTokenResult => {
  const session = useSession();
  const { data: toolset } = useToolset(
    { slug: toolsetSlug },
    {},
    { enabled: !!toolsetSlug },
  );

  const externalMcpOAuthConfig: { issuer: string } | undefined = useMemo(() => {
    let firstToolWithOAuth: ExternalMCPToolDefinition | undefined = undefined;
    for (const tool of toolset?.tools ?? []) {
      if (
        tool.externalMcpToolDefinition?.requiresOauth &&
        tool.externalMcpToolDefinition.oauthVersion !== "none"
      ) {
        firstToolWithOAuth = tool.externalMcpToolDefinition;
      }
    }

    if (!firstToolWithOAuth) return undefined;
    if (!firstToolWithOAuth.oauthTokenEndpoint) return undefined;

    return {
      issuer: new URL(firstToolWithOAuth.oauthTokenEndpoint).origin,
    };
  }, [toolset]);

  // Fetch OAuth token from the backend
  const {
    data: tokenResponse,
    isLoading,
    error,
    refetch,
  } = useQuery<OAuthTokenResponse, Error>({
    queryKey: ["playground.oauthToken", toolsetSlug],
    queryFn: async (): Promise<OAuthTokenResponse> => {
      const params = new URLSearchParams({
        toolset_id: toolset?.id ?? "",
        issuer: externalMcpOAuthConfig?.issuer ?? "",
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/token?${params.toString()}`,
        {
          method: "GET",
          headers: {
            "Gram-Session": session.session,
            ...sessionHeaders,
            "Content-Type": "application/json",
          },
        },
      );

      let status: OAuthStatus;
      if (!response.ok) {
        switch (response.status) {
          case 404:
            status = "unauthenticated";
            break;
          case 401:
            status = "expired";
            break;
          default:
            throw new Error(
              `Failed to get OAuth token: ${await response.text()}`,
            );
        }
      } else {
        status = "authenticated";
      }

      return {
        status,
        ...(await response.json()),
      };
    },
    enabled: externalMcpOAuthConfig !== undefined,
    staleTime: 5 * 60 * 1000, // Token considered stale after 5 minutes
    gcTime: 10 * 60 * 1000, // Keep in cache for 10 minutes
  });

  // Parse expiration time if present
  const expiresAt = useMemo(() => {
    if (tokenResponse?.expires_at) {
      return new Date(tokenResponse.expires_at);
    }
    return null;
  }, [tokenResponse?.expires_at]);

  const handleRefetch = async () => {
    await refetch();
  };

  return {
    required: externalMcpOAuthConfig !== undefined,
    oauthStatus: tokenResponse?.status ?? null,
    accessToken: tokenResponse?.access_token ?? null,
    tokenType: tokenResponse?.token_type ?? null,
    isLoading,
    error: error?.message ?? null,
    expiresAt,
    scope: tokenResponse?.scope ?? null,
    refetch: handleRefetch,
  };
};
