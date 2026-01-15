import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useGramElements } from "@gram-ai/elements";
import { type FC } from "react";
import { ThreadPrimitive, MessagePrimitive } from "@assistant-ui/react";

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
