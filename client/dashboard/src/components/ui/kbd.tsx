import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Single keyboard-key chip — Claude Design treatment (mono, hairline border,
 * bg-muted, squared corners).
 *
 * Deliberately does not delegate rendering to the vendored `Key`
 * (`@/components/ui/key-hint`, read before writing this): that component
 * renders a rounded-lg chip with a background *gradient* and drop shadow,
 * which conflicts with the flat hairline treatment here and can't be cleanly
 * overridden via `className` — the gradient is a `background-image` layered
 * on top of any `background-color` override, so it stays visible underneath.
 * `Kbd` is the canonical keyboard-hint chip for new code; prefer it over
 * reaching for `Key`/`KeyHint` directly.
 */
export interface KbdProps extends React.HTMLAttributes<HTMLElement> {
  className?: string;
}

export function Kbd({
  className,
  children,
  ...props
}: KbdProps): React.JSX.Element {
  return (
    <kbd
      className={cn(
        "border-neutral-softest bg-muted text-muted-foreground inline-flex h-5 min-w-5 items-center justify-center border px-1 font-mono text-[10px] leading-none",
        className,
      )}
      {...props}
    >
      {children}
    </kbd>
  );
}

export interface KbdSequenceProps {
  /** Ordered keys/modifiers to render, e.g. `["⌘", "K"]`. */
  keys: React.ReactNode[];
  /** Separator rendered between chips. Defaults to "+". */
  separator?: React.ReactNode;
  className?: string;
}

/** Multiple `Kbd` chips joined by a muted separator, e.g. ⌘+K. */
export function KbdSequence({
  keys,
  separator = "+",
  className,
}: KbdSequenceProps): React.JSX.Element {
  return (
    <span className={cn("inline-flex items-center gap-1", className)}>
      {keys.map((key, index) => (
        <React.Fragment key={index}>
          <Kbd>{key}</Kbd>
          {index < keys.length - 1 && (
            <span
              aria-hidden="true"
              className="text-muted-foreground font-mono text-[10px]"
            >
              {separator}
            </span>
          )}
        </React.Fragment>
      ))}
    </span>
  );
}
