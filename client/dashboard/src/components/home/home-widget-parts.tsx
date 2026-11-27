/** Shared pieces for the project homepage widgets and sections. */
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import { type ReactNode } from "react";
import { useAskTerminal } from "./home-widget-hooks";
import { Link } from "react-router";

// Widgets live in fixed-height grid rows on wide screens: the card clips and
// its body scrolls.
export const WIDGET_CLASS = "min-h-0 overflow-hidden";
// Home widget title bars sit a little tighter than the default card header.
export const WIDGET_HEADER_CLASS = "py-3";
export const WIDGET_BODY_CLASS = "min-h-0 flex-1 overflow-y-auto";

/** One row that sends its question to the terminal beside every widget. */
export function ActionRow({
  children,
  prompt,
}: {
  children: ReactNode;
  prompt: string;
}): JSX.Element {
  const sendPrompt = useAskTerminal();
  return (
    <button
      type="button"
      className="group hover:bg-muted/50 flex w-full items-start gap-3 px-6 py-3 text-left"
      onClick={() => sendPrompt(prompt)}
    >
      {children}
      <span className="text-muted-foreground mt-0.5 shrink-0 text-xs opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100">
        Ask ›
      </span>
    </button>
  );
}

export function AskButton({
  prompt,
  className,
}: {
  prompt: string;
  className?: string;
}): JSX.Element {
  const sendPrompt = useAskTerminal();
  return (
    <button
      type="button"
      onClick={() => sendPrompt(prompt)}
      className={cn(
        "text-muted-foreground hover:text-foreground shrink-0 text-xs",
        className,
      )}
    >
      Ask ›
    </button>
  );
}

export function ViewAllLink({ to }: { to: string }): JSX.Element {
  return (
    <Link
      to={to}
      className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-xs no-underline"
    >
      View all
      <ChevronRight className="size-3" />
    </Link>
  );
}

export function ListSkeleton(): JSX.Element {
  return (
    <div className="space-y-3 px-6 py-5">
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  );
}

export const LOAD_ERROR = "Couldn't load this data. Try reloading the page.";

export function LoadError(): JSX.Element {
  return (
    <p className="text-muted-foreground px-6 py-5 text-sm">{LOAD_ERROR}</p>
  );
}

export function LogsOff(): JSX.Element {
  return (
    <p className="text-muted-foreground px-6 py-5 text-sm">
      Turn on logs for this organization to see activity.
    </p>
  );
}

/**
 * One widget in the slot under the chat. Every slot widget sits in the same
 * place; only the active one shows, and the switch is a crossfade.
 */
export function SlotItem({
  id,
  active,
  children,
}: {
  id: string;
  active: boolean;
  children: ReactNode;
}): JSX.Element {
  return (
    <div
      data-home-lead={id}
      inert={!active}
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 transition-opacity duration-300 motion-reduce:transition-none",
        active ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
      {children}
    </div>
  );
}
