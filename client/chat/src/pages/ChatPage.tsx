import { useEffect, useState } from "react";
import { useParams } from "react-router";
import {
  Chat,
  GramElementsProvider,
  type ElementsConfig,
} from "@gram-ai/elements";
import { ChatHeader } from "@/components/ChatHeader";
import { useAuth } from "@/contexts/AuthContext";

interface HostedChatConfig {
  id: string;
  name: string;
  slug: string;
  projectSlug?: string;
  mcpSlug?: string;
  systemPrompt?: string;
  welcomeTitle?: string;
  welcomeSubtitle?: string;
  themeColorScheme: string;
}

export function ChatPage() {
  const { chatSlug } = useParams<{ chatSlug: string }>();
  const { session } = useAuth();
  const [config, setConfig] = useState<HostedChatConfig | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetchConfig() {
      if (!chatSlug) {
        setError("No chat specified");
        setLoading(false);
        return;
      }

      try {
        const res = await fetch(
          `/rpc/hostedChats.getPublic?chat_slug=${encodeURIComponent(chatSlug)}`,
          {
            credentials: "include",
            headers: {
              "Content-Type": "application/json",
            },
          },
        );

        if (res.status === 404) {
          setError("Chat not found");
          setLoading(false);
          return;
        }

        if (res.status === 403) {
          setError("You do not have access to this chat");
          setLoading(false);
          return;
        }

        if (!res.ok) {
          setError("Failed to load chat configuration");
          setLoading(false);
          return;
        }

        const data = await res.json();
        setConfig({
          id: data.hosted_chat.id,
          name: data.hosted_chat.name,
          slug: data.hosted_chat.slug,
          projectSlug: data.project_slug,
          mcpSlug: data.hosted_chat.mcp_slug,
          systemPrompt: data.hosted_chat.system_prompt,
          welcomeTitle: data.hosted_chat.welcome_title,
          welcomeSubtitle: data.hosted_chat.welcome_subtitle,
          themeColorScheme: data.hosted_chat.theme_color_scheme,
        });
        setLoading(false);
      } catch {
        setError("Failed to load chat configuration");
        setLoading(false);
      }
    }

    fetchConfig();
  }, [chatSlug]);

  if (loading) {
    return (
      <div className="flex h-full flex-col bg-neutral-950">
        <ChatHeader />
        <div className="flex flex-1 items-center justify-center">
          <div className="text-neutral-400">Loading chat...</div>
        </div>
      </div>
    );
  }

  if (error || !config) {
    return (
      <div className="flex h-full flex-col bg-neutral-950">
        <ChatHeader />
        <div className="flex flex-1 flex-col items-center justify-center text-white">
          <p className="text-lg font-medium">{error || "Chat not found"}</p>
          <p className="mt-2 text-sm text-neutral-400">
            Check the URL and try again, or contact the project owner.
          </p>
        </div>
      </div>
    );
  }

  // All API calls use relative URLs, proxied through chat domain's ingress
  const origin = window.location.origin;
  const projectSlug = config.projectSlug || "";
  const mcpURL = config.mcpSlug
    ? `${origin}/mcp/${projectSlug}/${config.mcpSlug}`
    : `${origin}/mcp/${projectSlug}`;

  const elementsConfig: ElementsConfig = {
    projectSlug,
    variant: "standalone",
    mcp: mcpURL,
    systemPrompt: config.systemPrompt,
    theme: {
      colorScheme:
        (config.themeColorScheme as "light" | "dark" | "system") || "system",
    },
    api: {
      url: origin,
      sessionFn: async () => {
        // Create a chat session token using the user's gram_session cookie
        const res = await fetch("/chat/session", {
          method: "POST",
          credentials: "include",
          headers: {
            "Content-Type": "application/json",
            "Gram-Project": projectSlug,
            ...(session ? { "Gram-Session": session } : {}),
          },
          body: JSON.stringify({
            embedOrigin: origin,
          }),
        });

        if (!res.ok) {
          throw new Error("Failed to create chat session");
        }

        const data = await res.json();
        return data.client_token;
      },
    },
    history: {
      enabled: true,
    },
  };

  // Add welcome screen config if provided
  if (config.welcomeTitle || config.welcomeSubtitle) {
    elementsConfig.welcome = {
      title: config.welcomeTitle,
      subtitle: config.welcomeSubtitle,
    };
  }

  return (
    <div className="flex h-full flex-col bg-neutral-950">
      <ChatHeader chatName={config.name} />
      <div className="flex-1 overflow-hidden">
        <GramElementsProvider config={elementsConfig}>
          <Chat />
        </GramElementsProvider>
      </div>
    </div>
  );
}
