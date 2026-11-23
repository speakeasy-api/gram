import { useInsightsState } from "@/components/insights-context";
import { useProject } from "@/contexts/Auth";
import { Chat } from "@/elements";
import { cn } from "@/lib/utils";
import { SquarePen } from "lucide-react";
import type { ReactElement } from "react";
import {
  TERMINAL_HOST_CLASS,
  TerminalSkinContext,
} from "./terminal-skin-theme";

/**
 * The Project Assistant as a terminal window on the project homepage. It
 * renders the shared runtime, so the conversation continues in the dock and
 * on /chat.
 */
export function HomeTerminal({
  className,
}: {
  className?: string;
}): ReactElement {
  const {
    assistantReady,
    assistantNeedsAdmin,
    assistantError,
    retryAssistant,
    newConversation,
  } = useInsightsState();
  const { slug } = useProject();

  return (
    <section
      aria-label="Project Assistant"
      className={cn(
        "bg-card flex min-h-0 flex-col overflow-hidden border",
        className,
      )}
    >
      <header className="flex shrink-0 items-center justify-between gap-4 border-b px-6 py-4">
        <h2 className="text-eyebrow">
          Project assistant{" "}
          <span className="normal-case tracking-normal">~/{slug}</span>
        </h2>
        {assistantReady && (
          <button
            type="button"
            onClick={newConversation}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs"
          >
            <SquarePen className="size-3.5" />
            New chat
          </button>
        )}
      </header>
      <div className="min-h-0 flex-1">
        {assistantNeedsAdmin ? (
          <p className="text-muted-foreground p-6 font-mono text-sm">
            Ask an admin to enable the Project Assistant for this project.
          </p>
        ) : assistantError ? (
          // The dock that would show this is hidden on Home.
          <p className="text-muted-foreground p-6 font-mono text-sm">
            {assistantError}{" "}
            <button
              type="button"
              onClick={retryAssistant}
              className="text-foreground underline underline-offset-2"
            >
              Try again
            </button>
          </p>
        ) : assistantReady ? (
          <TerminalSkinContext.Provider value>
            <div
              data-radius="sharp"
              className={cn(TERMINAL_HOST_CLASS, "h-full")}
            >
              <Chat />
            </div>
          </TerminalSkinContext.Provider>
        ) : (
          <p className="text-muted-foreground animate-pulse p-6 font-mono text-sm">
            Starting the Project Assistant…
          </p>
        )}
      </div>
    </section>
  );
}
