import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  MessagePrimitive,
  ThreadPrimitive,
  useGramElements,
} from "@gram-ai/elements";
import { type FC } from "react";
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
