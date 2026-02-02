import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { ThreadPrimitive } from "@assistant-ui/react";
import { useGramElements } from "@gram-ai/elements";
import type { FC } from "react";

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
              prompt={suggestion.prompt}
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

