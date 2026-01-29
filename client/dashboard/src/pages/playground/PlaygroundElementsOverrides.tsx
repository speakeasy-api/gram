import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { ComposerPrimitive, ThreadPrimitive } from "@assistant-ui/react";
import { useGramElements } from "@gram-ai/elements";
import { AlertCircle, ArrowDown, ArrowUp, Square } from "lucide-react";
import { createContext, type FC, useContext } from "react";

// Context for passing auth warning to the Composer component
type AuthWarningValue = { missingCount: number; toolsetSlug: string } | null;
export const PlaygroundAuthWarningContext =
  createContext<AuthWarningValue>(null);
export const usePlaygroundAuthWarning = () =>
  useContext(PlaygroundAuthWarningContext);

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

/**
 * Custom Composer component using Gram design system.
 * Includes auth warning banner, scroll-to-bottom button, and send/cancel actions.
 */
export const GramComposer: FC = () => {
  const authWarning = usePlaygroundAuthWarning();
  const routes = useRoutes();

  return (
    <div className="bg-background sticky bottom-0 flex w-full flex-col gap-2 py-3 rounded-2xl">
      {/* Scroll to bottom button - uses native button to avoid dashboard Button's preventDefault */}
      <ThreadPrimitive.ScrollToBottom asChild>
        <button
          type="button"
          className="absolute -top-12 z-10 self-center rounded-full border bg-background p-2 shadow-xs hover:bg-accent disabled:invisible dark:bg-background dark:border-input dark:hover:bg-accent"
          aria-label="Scroll to bottom"
        >
          <ArrowDown className="size-4" />
        </button>
      </ThreadPrimitive.ScrollToBottom>

      {/* Auth warning */}
      {authWarning && (
        <div className="flex items-center gap-2 px-3 py-2 bg-warning/10 border border-warning/20 rounded-md text-sm text-warning-foreground">
          <AlertCircle className="size-4 shrink-0" />
          <span>
            {authWarning.missingCount} authentication{" "}
            {authWarning.missingCount === 1 ? "variable" : "variables"} not
            configured.{" "}
            <routes.mcp.details.Link
              params={[authWarning.toolsetSlug]}
              hash="auth"
              className="underline hover:text-foreground font-medium"
            >
              Configure now
            </routes.mcp.details.Link>
          </span>
        </div>
      )}

      {/* Composer input */}
      <ComposerPrimitive.Root className="group/input-group border-input bg-background has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-ring/5 dark:bg-background relative flex w-full flex-col border px-1 pt-2 shadow-xs transition-[color,box-shadow] outline-none has-[textarea:focus-visible]:ring-1 rounded-2xl">
        <ComposerPrimitive.Input
          placeholder="Send a message..."
          className="placeholder:text-muted-foreground mb-1 max-h-32 w-full resize-none bg-transparent px-3.5 pt-1.5 pb-3 outline-none focus-visible:ring-0 min-h-12 text-base"
          rows={1}
          autoFocus
          aria-label="Message input"
        />

        {/* Action buttons */}
        <div className="relative mx-1 mt-2 mb-2 flex items-center justify-between">
          <Type small muted className="ml-2">
            Powered by{" "}
            <routes.elements.Link className="font-medium">
              Gram Elements
            </routes.elements.Link>
          </Type>
          <ThreadPrimitive.If running={false}>
            <ComposerPrimitive.Send asChild>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="submit"
                    variant="default"
                    size="icon"
                    className="size-[34px] p-1 rounded-full"
                    aria-label="Send message"
                  >
                    <ArrowUp className="size-5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Send message</TooltipContent>
              </Tooltip>
            </ComposerPrimitive.Send>
          </ThreadPrimitive.If>

          <ThreadPrimitive.If running>
            <ComposerPrimitive.Cancel asChild>
              <Button
                type="button"
                variant="default"
                size="icon"
                className="border-muted-foreground/60 hover:bg-primary/75 dark:border-muted-foreground/90 size-[34px] border rounded-full"
                aria-label="Stop generating"
              >
                <Square className="size-3.5 fill-white dark:fill-black" />
              </Button>
            </ComposerPrimitive.Cancel>
          </ThreadPrimitive.If>
        </div>
      </ComposerPrimitive.Root>
    </div>
  );
};
