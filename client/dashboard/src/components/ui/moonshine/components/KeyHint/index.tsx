import { cn } from "@/components/ui/moonshine/lib/utils";
import { Icon } from "@/components/ui/moonshine/components/Icon";
import React from "react";

type Modifier = "shift" | "ctrlorcommand" | "alt" | "meta" | "esc";

function checkIsMac(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return /Mac|iPod|iPhone|iPad/.test(window.navigator.platform);
}

const modifierMap: Record<Modifier, string> = {
  shift: "⇧",
  ctrlorcommand: checkIsMac() ? "⌘" : "Ctrl",
  alt: checkIsMac() ? "⌥" : "Alt",
  meta: checkIsMac() ? "⌘" : "Win",
  esc: "Esc",
};

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface KeyProps {
  value: string;
  className?: string;
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export function Key({ value, className }: KeyProps): React.JSX.Element {
  return (
    <span
      className={cn(
        "rounded-lg border bg-gradient-to-br from-background to-card px-2 py-0.5 text-sm text-foreground/70 dark:text-foreground",
        className,
      )}
    >
      {value}
    </span>
  );
}

interface KeyHintItemProps {
  modifiers: Modifier[];
  keys: string[];
  actionText?: string;
}

function KeyHintKeys({ modifiers, keys }: KeyHintItemProps) {
  return (
    <div className="flex flex-row items-center gap-1">
      {modifiers.map((modifier, index) => (
        <React.Fragment key={`${modifier}-${index}`}>
          <Key value={modifierMap[modifier]} />
          {index < modifiers.length - 1 && (
            <span className="text-sm text-body-muted">+</span>
          )}
        </React.Fragment>
      ))}
      {keys.length > 0 && <span className="text-sm text-body-muted">+</span>}
      {keys.map((key, index) => (
        <React.Fragment key={`${key}-${index}`}>
          <Key value={key.toUpperCase()} />
          {index < keys.length - 1 && (
            <span className="text-sm text-body-muted">+</span>
          )}
        </React.Fragment>
      ))}
    </div>
  );
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export interface KeyHintProps extends React.HTMLAttributes<HTMLDivElement> {
  modifiers: Modifier[];
  keys: string[];
  actionText: string;
  titleText?: string;
  dismissable?: boolean;
  onDismiss?: () => void;
}

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export function KeyHint({
  modifiers,
  keys,
  actionText,
  className,
  dismissable = true,
  onDismiss,
  titleText = "Key hint",
  ...props
}: KeyHintProps): React.JSX.Element {
  return (
    <div
      className={cn(
        "inline-flex min-w-24 flex-col items-start gap-1 rounded-lg border text-base font-semibold tracking-tight text-black shadow-sm shadow-black/5 select-none dark:text-white dark:shadow-white/10",
        className,
      )}
      {...props}
    >
      <div className="flex w-full flex-row items-center self-start border-b px-2.5 py-0.5 text-[10px] font-semibold tracking-wide text-body-muted uppercase select-none dark:text-body-muted/80">
        <div>{titleText}</div>
        {dismissable && (
          <button
            type="button"
            className="ml-auto cursor-pointer hover:text-foreground"
            onClick={onDismiss}
            aria-label="Dismiss"
          >
            <Icon name="x" className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <div className="flex flex-row items-center gap-1 px-4 py-3.5">
        <KeyHintKeys modifiers={modifiers} keys={keys} />
        {actionText && (
          <div className="text-sm font-normal text-body-muted">
            {actionText}
          </div>
        )}
      </div>
    </div>
  );
}
