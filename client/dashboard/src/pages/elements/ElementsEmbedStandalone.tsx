import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";
import { useEffect, useState } from "react";

type EmbedConfig = {
  mcp: string;
  projectSlug: string;
  apiUrl: string;
  systemPrompt?: string;
  variant?: "widget" | "sidecar" | "standalone";
  theme?: {
    colorScheme?: "light" | "dark" | "system";
    density?: "compact" | "normal" | "spacious";
    radius?: "round" | "soft" | "sharp";
  };
  welcome?: {
    title?: string;
    subtitle?: string;
  };
  composer?: {
    placeholder?: string;
  };
  model?: {
    showModelPicker?: boolean;
  };
  modal?: {
    title?: string;
    position?: "bottom-right" | "bottom-left" | "top-right" | "top-left";
    defaultOpen?: boolean;
  };
  tools?: {
    expandToolGroupsByDefault?: boolean;
  };
};

/**
 * Completely standalone embed component.
 * Receives configuration via postMessage from parent window.
 * No dependency on dashboard contexts - only loads elements CSS.
 */
export function ElementsEmbedStandalone() {
  const [config, setConfig] = useState<EmbedConfig | null>(null);
  const [sessionToken, setSessionToken] = useState<string | null>(null);

  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      // Only accept messages from same origin
      if (event.origin !== window.location.origin) return;

      if (event.data?.type === "ELEMENTS_CONFIG") {
        setConfig(event.data.config);
      }
      if (event.data?.type === "ELEMENTS_SESSION") {
        setSessionToken(event.data.sessionToken);
      }
    };

    window.addEventListener("message", handleMessage);

    // Signal to parent that we're ready
    window.parent.postMessage(
      { type: "ELEMENTS_READY" },
      window.location.origin
    );

    return () => window.removeEventListener("message", handleMessage);
  }, []);

  const gradientStyle = {
    background: "linear-gradient(135deg, #89CFF0 0%, #5DADE2 25%, #3498DB 50%, #85C1E9 75%, #AED6F1 100%)",
  };

  if (!config || !sessionToken) {
    return (
      <div
        style={{
          height: "100vh",
          width: "100vw",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontFamily: "system-ui",
          color: "rgba(255, 255, 255, 0.9)",
          ...gradientStyle,
        }}
      >
        Loading...
      </div>
    );
  }

  const elementsConfig: ElementsConfig = {
    projectSlug: config.projectSlug,
    mcp: config.mcp,
    api: {
      url: config.apiUrl,
      sessionFn: async () => sessionToken,
    },
    systemPrompt: config.systemPrompt,
    variant: config.variant || "standalone",
    theme: config.theme,
    welcome: config.welcome?.title
      ? {
          title: config.welcome.title,
          subtitle: config.welcome.subtitle || "",
        }
      : undefined,
    composer: config.composer,
    model: config.model,
    modal: config.modal,
    tools: config.tools,
  };

  const isStandalone = config.variant === "standalone" || !config.variant;

  // For widget/sidecar: full gradient background, chat floats on top
  // For standalone: gradient forms a frame/border around the chat
  if (isStandalone) {
    return (
      <div
        style={{
          height: "100vh",
          width: "100vw",
          padding: "24px",
          boxSizing: "border-box",
          ...gradientStyle,
        }}
      >
        <div
          style={{
            height: "100%",
            width: "100%",
            borderRadius: "12px",
            overflow: "hidden",
            boxShadow: "0 8px 32px rgba(0, 0, 0, 0.15)",
          }}
        >
          <GramElementsProvider config={elementsConfig}>
            <Chat />
          </GramElementsProvider>
        </div>
      </div>
    );
  }

  // Widget or Sidecar - full gradient background
  return (
    <div
      style={{
        height: "100vh",
        width: "100vw",
        ...gradientStyle,
      }}
    >
      <GramElementsProvider config={elementsConfig}>
        <Chat />
      </GramElementsProvider>
    </div>
  );
}
