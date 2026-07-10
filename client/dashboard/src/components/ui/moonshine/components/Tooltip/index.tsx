"use client";

import * as React from "react";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";

import { cn } from "@/components/ui/moonshine/lib/utils";

const TooltipProvider = TooltipPrimitive.Provider;

interface TooltipProps {
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

const TooltipTrigger = TooltipPrimitive.Trigger;

const TooltipPortal = TooltipPrimitive.Portal;

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 4, ...props }, ref) => (
  <TooltipPrimitive.Content
    ref={ref}
    sideOffset={sideOffset}
    className={cn(
      "z-50 animate-in overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-sm text-popover-foreground fade-in-0 zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
      className,
    )}
    {...props}
  />
));
Tooltip.displayName = "Tooltip";
TooltipContent.displayName = TooltipPrimitive.Content.displayName;

export {
  Tooltip,
  TooltipPortal,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
};
