"use client";

import * as React from "react";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";

import { cn } from "@/lib/utils";

function TooltipProvider({
  delayDuration = 0,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>): React.JSX.Element {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  );
}

export interface TooltipProps {
  children?: React.ReactNode;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /**
   * The duration from when the pointer enters the trigger until the tooltip
   * opens. This overrides the TooltipProvider value.
   * @defaultValue 700
   */
  delayDuration?: number;
  /**
   * When true, the tooltip closes as the pointer leaves the trigger instead of
   * allowing hover on the tooltip content.
   * @defaultValue false
   */
  disableHoverableContent?: boolean;
}

const Tooltip = (props: TooltipProps): React.JSX.Element => (
  <TooltipPrimitive.Root {...props} />
);
Tooltip.displayName = "Tooltip";

const TooltipTrigger = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Trigger>
>((props, ref) => (
  <TooltipPrimitive.Trigger ref={ref} data-slot="tooltip-trigger" {...props} />
));
TooltipTrigger.displayName = TooltipPrimitive.Trigger.displayName;

const TooltipArrow = TooltipPrimitive.TooltipArrow;

const TooltipPortal = TooltipPrimitive.Portal;

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content> & {
    /** Light surface in light mode instead of the default inverted one. */
    inverted?: boolean;
  }
>(
  (
    { className, sideOffset = 4, inverted = false, children, ...props },
    ref,
  ) => (
    // Portalled to `document.body` so tooltips escape scroll and overflow
    // containers — table cells and sheets clip them otherwise.
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        ref={ref}
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 animate-in border bg-popover px-3 py-1.5 text-sm text-popover-foreground shadow-md fade-in-0 zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          // `break-words` so an unbreakable token (a content hash, a URL)
          // wraps inside max-w-sm instead of stretching the box past it.
          "max-w-sm text-pretty break-words",
          inverted && "bg-background [&_svg]:fill-background dark:border-1",
          className,
        )}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="fill-popover" />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  ),
);
TooltipContent.displayName = TooltipPrimitive.Content.displayName;

/** Trigger + content in one call, for the common "hover this, show text" case. */
function SimpleTooltip({
  tooltip,
  children,
}: {
  tooltip: React.ReactNode;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export {
  SimpleTooltip,
  Tooltip,
  TooltipArrow,
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipTrigger,
};
