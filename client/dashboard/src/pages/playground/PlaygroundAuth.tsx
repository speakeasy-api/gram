import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import {
  ExternalOAuthServer,
  OAuthProxyServer,
} from "@gram/client/models/components";
import { KeyRoundIcon, Loader2Icon, CheckCircleIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

interface PlaygroundAuthProps {
  toolset: Toolset & {
    externalOauthServer?: ExternalOAuthServer;
    oauthProxyServer?: OAuthProxyServer;
    mcpSlug?: string;
  };
  environment?: {
    slug: string;
    entries?: Array<{ name: string; value: string }>;
  };
}

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY", "TOKEN", "PASSWORD"] as const;
const PASSWORD_MASK = "••••••••";

export function getAuthStatus(
  toolset: Pick<
    Toolset,
    "securityVariables" | "serverVariables" | "functionEnvironmentVariables"
  >,
  environment?: { entries?: Array<{ name: string; value: string }> },
): { hasMissingAuth: boolean; missingCount: number } {
  // In playground, always filter out server_url variables since they can't be configured here
  const relevantEnvVars = [
    ...(toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      []),
    ...(toolset?.serverVariables?.flatMap((serverVar) =>
      serverVar.envVariables.filter(
        (v) => !v.toLowerCase().includes("server_url"),
      ),
    ) ?? []),
    ...(toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ??
      []),
  ];

  const missingCount = relevantEnvVars.filter((varName) => {
    const entry = environment?.entries?.find((e) => e.name === varName);
    return !entry?.value || entry.value.trim() === "";
  }).length;

  return {
    hasMissingAuth: missingCount > 0,
    missingCount,
  };
}

/**
 * Checks if a toolset has OAuth configured (either OAuth proxy or external OAuth server)
 */
export function hasOAuthConfigured(
  toolset: Pick<PlaygroundAuthProps["toolset"], "externalOauthServer" | "oauthProxyServer">,
): boolean {
  return !!(toolset.externalOauthServer || toolset.oauthProxyServer);
}

/**
 * Hook to manage OAuth authentication flow for the playground
 */
function useOAuthFlow(toolset: PlaygroundAuthProps["toolset"]) {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();
  const [isAuthenticating, setIsAuthenticating] = useState(false);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const popupRef = useRef<Window | null>(null);

  // Listen for OAuth completion messages
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      // Verify origin for security
      const serverURL = getServerURL();
      if (!serverURL || !event.origin.startsWith(new URL(serverURL).origin)) {
        return;
      }

      if (event.data?.type === "gram-oauth-complete") {
        if (event.data.success) {
          setIsAuthenticated(true);
          toast.success("Successfully authenticated");
        } else {
          toast.error(event.data.error || "Authentication failed");
        }
        setIsAuthenticating(false);
        popupRef.current?.close();
        popupRef.current = null;
      }
    };

    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, []);

  const startOAuthFlow = useCallback(async () => {
    if (!toolset.mcpSlug) {
      toast.error("Toolset MCP slug not available");
      return;
    }

    setIsAuthenticating(true);

    try {
      // Create a chat session to get the token
      const sessionResult = await createSessionMutation.mutateAsync({
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

      const clientToken = sessionResult.clientToken;

      // Fetch the OAuth authorization URL
      const serverURL = getServerURL();
      const response = await fetch(
        `${serverURL}/oauth/${toolset.mcpSlug}/session-authorize-url?origin=${encodeURIComponent(window.location.origin)}`,
        {
          headers: {
            "Gram-Chat-Session": clientToken,
          },
        },
      );

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || "Failed to get authorization URL");
      }

      const data = await response.json();
      const authorizationUrl = data.authorization_url;

      if (!authorizationUrl) {
        throw new Error("No authorization URL returned");
      }

      // Open popup for OAuth flow
      const width = 500;
      const height = 600;
      const left = window.screenX + (window.outerWidth - width) / 2;
      const top = window.screenY + (window.outerHeight - height) / 2;

      popupRef.current = window.open(
        authorizationUrl,
        "gram-oauth",
        `width=${width},height=${height},left=${left},top=${top},popup=yes`,
      );

      // Check if popup was blocked
      if (!popupRef.current) {
        throw new Error("Popup was blocked. Please allow popups for this site.");
      }

      // Poll to check if popup was closed without completing
      const checkClosed = setInterval(() => {
        if (popupRef.current?.closed) {
          clearInterval(checkClosed);
          if (isAuthenticating) {
            setIsAuthenticating(false);
          }
        }
      }, 500);
    } catch (error) {
      setIsAuthenticating(false);
      toast.error(
        error instanceof Error ? error.message : "Failed to start authentication",
      );
    }
  }, [
    toolset.mcpSlug,
    createSessionMutation,
    project.id,
    project.slug,
    session.session,
    session.user.id,
    isAuthenticating,
  ]);

  return {
    isAuthenticating,
    isAuthenticated,
    startOAuthFlow,
  };
}

export function PlaygroundAuth({ toolset, environment }: PlaygroundAuthProps) {
  const routes = useRoutes();
  const hasOAuth = hasOAuthConfigured(toolset);
  const { isAuthenticating, isAuthenticated, startOAuthFlow } = useOAuthFlow(toolset);

  const relevantEnvVars = useMemo(() => {
    const securityVars =
      toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      [];
    // In playground, always filter out server_url variables since they can't be configured here
    const serverVars =
      toolset?.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter(
          (v) => !v.toLowerCase().includes("server_url"),
        ),
      ) ?? [];
    const functionEnvVars =
      toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ?? [];

    return [...securityVars, ...serverVars, ...functionEnvVars];
  }, [
    toolset?.securityVariables,
    toolset?.serverVariables,
    toolset.functionEnvironmentVariables,
  ]);

  // Show OAuth section if configured
  const oauthSection = hasOAuth ? (
    <div className="space-y-2 pb-3 border-b border-border">
      <div className="flex items-center justify-between">
        <Label className="text-xs font-medium">OAuth Authentication</Label>
        {isAuthenticated && (
          <span className="text-xs text-green-600 flex items-center gap-1">
            <CheckCircleIcon className="size-3" />
            Connected
          </span>
        )}
      </div>
      <Button
        size="sm"
        variant={isAuthenticated ? "outline" : "default"}
        className="w-full"
        onClick={startOAuthFlow}
        disabled={isAuthenticating}
      >
        {isAuthenticating ? (
          <>
            <Loader2Icon className="size-4 mr-2 animate-spin" />
            Authenticating...
          </>
        ) : isAuthenticated ? (
          <>
            <KeyRoundIcon className="size-4 mr-2" />
            Re-authenticate
          </>
        ) : (
          <>
            <KeyRoundIcon className="size-4 mr-2" />
            Authenticate
          </>
        )}
      </Button>
      <Type variant="small" className="text-muted-foreground">
        {toolset.oauthProxyServer
          ? "Connect your account to use this toolset"
          : "Sign in with the external service"}
      </Type>
    </div>
  ) : null;

  if (relevantEnvVars.length === 0 && !hasOAuth) {
    return (
      <div className="text-center py-4">
        <Type variant="small" className="text-muted-foreground">
          No authentication required
        </Type>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {oauthSection}

      {relevantEnvVars.length > 0 && (
        <>
          {relevantEnvVars.map((varName) => {
            const entry =
              environment?.entries?.find((e) => e.name === varName) ?? null;
            const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
              varName.toUpperCase().includes(indicator),
            );
            const hasExistingValue =
              entry?.value != null && entry.value.trim() !== "";
            const displayValue = hasExistingValue
              ? isSecret
                ? PASSWORD_MASK
                : entry.value
              : "";

            return (
              <div key={varName} className="space-y-1.5">
                <Label htmlFor={`auth-${varName}`} className="text-xs font-medium">
                  {varName}
                </Label>
                <Input
                  id={`auth-${varName}`}
                  value={displayValue}
                  placeholder={hasExistingValue ? "Configured" : "Not set"}
                  type={isSecret ? "password" : "text"}
                  className="font-mono text-xs h-7"
                  readOnly
                  disabled
                />
              </div>
            );
          })}
          <Type variant="small" className="text-muted-foreground pt-2">
            Configure auth in the{" "}
            <routes.toolsets.toolset.Link
              params={[toolset.slug]}
              hash="auth"
              className="underline hover:text-foreground"
            >
              toolset settings
            </routes.toolsets.toolset.Link>
          </Type>
        </>
      )}
    </div>
  );
}
