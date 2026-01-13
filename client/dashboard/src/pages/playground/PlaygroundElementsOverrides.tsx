import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from "@/components/ai-elements/prompt-input";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import {
  useGramElements,
  ThreadPrimitive,
  MessagePrimitive,
  useAssistantApi,
  useAssistantState,
} from "@gram-ai/elements";
import { type FC } from "react";
import { usePlaygroundAuthWarning } from "./PlaygroundElements";
import { useRoutes } from "@/routes";
import { AlertCircle } from "lucide-react";
import { useTelemetry } from "@/contexts/Telemetry";
import { toast } from "sonner";

/**
 * Custom ThreadWelcome component using Gram design system.
 * Displays centered empty state with title, subtitle, and optional suggestions.
 */
export const GramThreadWelcome: FC = () => {
  const { config } = useGramElements();
  const { title, subtitle, suggestions } = config.welcome ?? {};

  return (
    <div className="flex size-full flex-col items-center justify-center gap-3 p-8 text-center">
      <div className="space-y-1">
        <Type variant="subheading" className="font-medium">
          {title}
        </Type>
        <Type variant="small" muted>
          {subtitle}
        </Type>
      </div>
      {suggestions && suggestions.length > 0 && (
        <div className="flex flex-wrap justify-center gap-2 mt-4">
          {suggestions.map((suggestion, index) => (
            <ThreadPrimitive.Suggestion
              key={index}
              prompt={suggestion.action}
              send
              asChild
            >
              <Button variant="outline" size="sm" className="rounded-full">
                {suggestion.title}
              </Button>
            </ThreadPrimitive.Suggestion>
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * Custom UserMessage component using Gram design system.
 * Displays user messages as primary-colored bubbles aligned to the right.
 */
export const GramUserMessage: FC = () => (
  <MessagePrimitive.Root asChild>
    <div className="flex w-full justify-end py-4 px-2" data-role="user">
      <div className="flex flex-col gap-2 overflow-hidden rounded-lg max-w-[80%] bg-secondary px-4 py-3 text-foreground">
        <MessagePrimitive.Content />
      </div>
    </div>
  </MessagePrimitive.Root>
);

export const Composer: FC = () => {
  const threadState = useAssistantState((s) => s.thread);
  const threadApi = useAssistantApi();
  const authWarning = usePlaygroundAuthWarning();
  const routes = useRoutes();

  const handleSubmit = () => {
    threadApi.composer().send();
  };

  const handleChange = (value: string) => {
    threadApi.composer().setText(value);
  };

  return (
    <div className="flex flex-col gap-3">
      {authWarning && (
        <div className="flex items-center gap-2 px-3 py-2 bg-warning/10 border border-warning/20 rounded-md text-sm text-warning-foreground">
          <AlertCircle className="size-4 shrink-0" />
          <span>
            {authWarning.missingCount} authentication{" "}
            {authWarning.missingCount === 1 ? "variable" : "variables"} not
            configured.{" "}
            <routes.toolsets.toolset.Link
              params={[authWarning.toolsetSlug]}
              hash="auth"
              className="underline hover:text-foreground font-medium"
            >
              Configure now
            </routes.toolsets.toolset.Link>
          </span>
        </div>
      )}
      <PromptInput onSubmit={handleSubmit}>
        <PromptInputBody>
          <PromptInputTextarea
            placeholder="Send a message..."
            onChange={handleChange}
          />
        </PromptInputBody>
        <PromptInputFooter className="bg-secondary border-t border-neutral-softest rounded-bl-lg rounded-br-lg">
          <PromptInputSubmit
            disabled={threadState.isLoading || threadState.isRunning}
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  );
};

/**
 * Share chat button that gets the chatId from Elements context.
 */
export const ShareChatButton: FC = () => {
  const { chatId } = useGramElements();
  const telemetry = useTelemetry();

  const handleShare = () => {
    const url = new URL(window.location.href);
    url.searchParams.set("chatId", chatId);

    telemetry.capture("chat_event", { action: "chat_shared" });
    navigator.clipboard.writeText(url.toString());
    toast.success("Chat link copied to clipboard");
  };

  return (
    <Button size="sm" variant="ghost" icon="link" onClick={handleShare}>
      Share chat
    </Button>
  );
};
