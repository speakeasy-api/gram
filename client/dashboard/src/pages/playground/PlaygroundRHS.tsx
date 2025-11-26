import { useRoutes } from "@/routes";
import { UIMessage } from "ai";
import { AlertCircle } from "lucide-react";
import { ChatConfig, ChatWindow } from "./ChatWindow";

export function PlaygroundRHS({
  configRef,
  initialPrompt,
  temperature,
  model,
  maxTokens,
  authWarning,
}: {
  configRef: ChatConfig;
  initialPrompt?: string | null;
  temperature: number;
  model: string;
  maxTokens: number;
  authWarning?: { missingCount: number; toolsetSlug: string } | null;
}) {
  const routes = useRoutes();

  const initialMessages: UIMessage[] = [
    {
      id: "1",
      role: "system",
      parts: [
        {
          type: "text",
          text: "This chat has access to the selected toolset on the left! Use it to test out your toolset.",
        },
      ],
    },
  ];

  return (
    <div className="h-full flex flex-col">
      <div className="flex-1 px-8 py-4 overflow-hidden">
        <ChatWindow
          configRef={configRef}
          initialMessages={initialMessages}
          initialPrompt={initialPrompt}
          initialTemperature={temperature}
          initialModel={model}
          initialMaxTokens={maxTokens}
          hideTemperatureSlider
          authWarning={
            authWarning ? (
              <div className="flex items-center gap-2 px-3 py-2 mb-3 bg-warning/10 border border-warning/20 rounded-md text-sm text-warning-foreground">
                <AlertCircle className="size-4 shrink-0" />
                <span>
                  {authWarning.missingCount} authentication{" "}
                  {authWarning.missingCount === 1 ? "variable" : "variables"}{" "}
                  not configured.{" "}
                  <routes.toolsets.toolset.Link
                    params={[authWarning.toolsetSlug]}
                    hash="auth"
                    className="underline hover:text-foreground font-medium"
                  >
                    Configure now
                  </routes.toolsets.toolset.Link>
                </span>
              </div>
            ) : undefined
          }
        />
      </div>
    </div>
  );
}
