import * as React from "react";

import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { cn } from "@/lib/utils";

/**
 * Two densities, both from the design system. "md" is the standalone choice a
 * settings panel gives a whole section to. "sm" is the same card inside a
 * dialog or a form step, where the choice is one field among several and the
 * full-size title competes with the step's own heading.
 */
export type RadioCardSize = "md" | "sm";

export type RadioCardGroupProps = Omit<
  React.ComponentProps<typeof RadioGroup>,
  "orientation" | "value"
> & {
  orientation?: "vertical" | "horizontal";
  value?: string | null;
  showIndicator?: boolean;
  size?: RadioCardSize;
};

export type RadioCardLabel = Exclude<
  React.ReactNode,
  null | undefined | boolean
>;

type RadioCardContent =
  | { title: RadioCardLabel; children?: React.ReactNode }
  | { title?: never; children: RadioCardLabel };

export type RadioCardProps = RadioCardContent & {
  value: string;
  disabled?: boolean;
  onSelect?: () => void;
  leading?: React.ReactNode;
  className?: string;
};

const INTERACTIVE_SELECTOR =
  'a, button, input, select, textarea, summary, label, [role=button], [role=link], [role=checkbox], [role=radio], [role=switch], [role=menuitem], [role=option], [contenteditable=true], [tabindex]:not([tabindex="-1"])';

const RadioCardGroupContext = React.createContext<{
  disabled: boolean;
  showIndicator: boolean;
  size: RadioCardSize;
}>({
  disabled: false,
  showIndicator: true,
  size: "md",
});

function hasLabelContent(node: React.ReactNode): boolean {
  return React.Children.toArray(node).some(
    (child) => typeof child !== "string" || child.trim().length > 0,
  );
}

export function RadioCardGroup({
  className,
  orientation = "vertical",
  value,
  disabled = false,
  showIndicator = true,
  size = "md",
  ...props
}: RadioCardGroupProps): React.JSX.Element {
  return (
    <RadioCardGroupContext.Provider value={{ disabled, showIndicator, size }}>
      <RadioGroup
        data-slot="radio-card-group"
        data-orientation={orientation}
        orientation={orientation}
        value={value === null ? "" : value}
        disabled={disabled}
        className={cn(
          "w-full",
          size === "sm" && "gap-2",
          orientation === "vertical"
            ? "grid-cols-1"
            : "grid-flow-col auto-cols-fr",
          className,
        )}
        {...props}
      />
    </RadioCardGroupContext.Provider>
  );
}

export function RadioCard({
  value,
  title,
  children,
  disabled = false,
  onSelect,
  leading,
  className,
}: RadioCardProps): React.JSX.Element {
  const id = React.useId();
  const group = React.useContext(RadioCardGroupContext);
  const keyboardActivationRef = React.useRef(false);
  const effectiveDisabled = disabled || group.disabled;
  const compact = group.size === "sm";
  const hasTitle = hasLabelContent(title);
  const hasChildren = hasLabelContent(children);

  if (!hasTitle && !hasChildren) {
    throw new Error(
      "RadioCard requires a non-empty title or children to provide an accessible label.",
    );
  }

  const titleId = `${id}-title`;
  const contentId = `${id}-content`;
  const labelId = hasTitle ? titleId : contentId;

  return (
    <div
      data-slot="radio-card"
      className={cn(
        "bg-card text-card-foreground flex min-w-0 cursor-pointer items-start gap-3 border shadow-xs transition-colors ease-in-out-quad",
        compact ? "px-4 py-3" : "p-4",
        "hover:bg-background/70 has-data-[state=checked]:border-primary hover:border-primary/50 has-data-[state=checked]:bg-background",
        "has-[[data-slot=radio-group-item]:focus-visible]:border-ring has-[[data-slot=radio-group-item]:focus-visible]:ring-ring/40 has-[[data-slot=radio-group-item]:focus-visible]:ring-1",
        "has-[[data-slot=radio-group-item]:disabled]:cursor-not-allowed has-[[data-slot=radio-group-item]:disabled]:opacity-50 has-[[data-slot=radio-group-item]:disabled]:hover:bg-card",
        className,
      )}
      onClick={(event) => {
        if (effectiveDisabled) return;

        const target = event.target;
        const interactive =
          target instanceof Element
            ? target.closest(INTERACTIVE_SELECTOR)
            : null;
        if (interactive && event.currentTarget.contains(interactive)) return;

        event.currentTarget
          .querySelector<HTMLElement>("[data-slot=radio-group-item]")
          ?.click();
      }}
    >
      <RadioGroupItem
        value={value}
        disabled={effectiveDisabled}
        aria-labelledby={labelId}
        aria-describedby={hasTitle && hasChildren ? contentId : undefined}
        className={cn("mt-0.5", !group.showIndicator && "sr-only")}
        onClick={() => {
          if (!keyboardActivationRef.current) onSelect?.();
        }}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;

          event.preventDefault();
          if (!event.repeat) {
            keyboardActivationRef.current = true;
            event.currentTarget.click();
          }
        }}
        onKeyUp={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;

          keyboardActivationRef.current = false;
          if (!event.repeat) onSelect?.();
        }}
      />
      {leading ? (
        // Centered on the title's line box, not on the top of the text
        // column: a 16px mark top-aligned against a 24px line sits high
        // enough to read as misaligned. Matches the mt-0.5 the radio uses.
        <div
          data-slot="radio-card-leading"
          className={cn("flex shrink-0 items-center", compact ? "h-5" : "h-6")}
        >
          {leading}
        </div>
      ) : null}
      <div className="min-w-0 flex-1">
        {hasTitle ? (
          <div
            id={titleId}
            className={cn("font-medium", compact ? "text-sm" : "text-base")}
          >
            {title}
          </div>
        ) : null}
        {hasChildren ? (
          <div
            id={contentId}
            className={cn(
              "text-base",
              hasTitle && "text-sm text-muted-foreground",
              hasTitle && (compact ? "mt-0.5" : "mt-1"),
            )}
          >
            {children}
          </div>
        ) : null}
      </div>
    </div>
  );
}
