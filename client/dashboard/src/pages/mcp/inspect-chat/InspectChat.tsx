import { useProject, useSession } from "@/contexts/Auth";
import { getChatMcpBaseURL } from "@/lib/utils";
import { Chat, GramElementsProvider, type Model } from "@/elements";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { useCallback } from "react";
import { toast } from "sonner";
import { GramThreadWelcome, NoMcpToolsBanner } from "./InspectChatOverrides";

interface InspectChatProps {
  /** Resolved MCP server URL (Speakeasy origin `/mcp/<slug>`). */
  mcpUrl: string;
  /**
   * User-session JWT forwarded as `Authorization: Bearer` so the runtime
   * gateway resolves the dashboard user's stored upstream credentials.
   */
  gatewayToken?: string;
  model: string;
}

/**
 * The MCP Inspect chat: the Speakeasy Elements chat, connected to one
 * MCP server, sized for a narrow panel beside the tool list.
 */
export function InspectChat({
  mcpUrl,
  gatewayToken,
  model,
}: InspectChatProps): JSX.Element {
  const session = useSession();
  const project = useProject();
  const createSessionMutation = useChatSessionsCreateMutation();
  const { theme: resolvedTheme } = useMoonshineConfig();

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

  return (
    <GramElementsProvider
      config={{
        projectSlug: project.slug,
        api: {
          url: getChatMcpBaseURL(),
          session: getSession,
          headers: {
            // Wire value kept from the retired Playground: the server bills
            // and attributes these completions to the "playground" key slot.
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
        },
        mcp: mcpUrl,
        variant: "standalone",
        model: {
          defaultModel: model as Model,
          showModelPicker: false,
        },
        welcome: {
          title: "Test your MCP server",
          subtitle:
            "This chat has access to the selected MCP server. Use it to test your tools.",
          // The panel sits beside the tool list, which already shows what is
          // available, so it skips the suggestion pill.
          suggestions: [],
        },
        composer: {
          placeholder: "Send a message…",
          toolMentions: true,
          requireMcpTools: true,
        },
        theme: {
          colorScheme: resolvedTheme === "dark" ? "dark" : "light",
          density: "normal",
          // Square corners, matching the detail page around it.
          radius: "sharp",
        },
        components: {
          ThreadWelcome: GramThreadWelcome,
        },
      }}
    >
      <div className="flex h-full min-h-0 flex-1 flex-col">
        <NoMcpToolsBanner />
        <div className="min-h-0 flex-1 overflow-hidden">
          <Chat />
        </div>
      </div>
    </GramElementsProvider>
  );
}
