"use client";

import { ComponentPropsWithRef, forwardRef } from "react";
import { Slottable } from "@radix-ui/react-slot";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/elements/components/ui/tooltip";
import { Button } from "@/elements/components/ui/button";
import { cn } from "@/lib/utils";

type TooltipIconButtonProps = ComponentPropsWithRef<typeof Button> & {
  tooltip: string;
  side?: "top" | "bottom" | "left" | "right";
  align?: "start" | "center" | "end";
};

export const TooltipIconButton = forwardRef<
  HTMLButtonElement,
  TooltipIconButtonProps
>(
  (
    {
      children,
      tooltip,
      side = "bottom",
      align = "center",
      className,
      disabled,
      ...rest
    },
    ref,
  ) => {
    const button = (
      <Button
        variant="ghost"
        size="icon"
        disabled={disabled}
        {...rest}
        className={cn("aui-button-icon size-6 p-1", className)}
        ref={ref}
      >
        <Slottable>{children}</Slottable>
        <span className="aui-sr-only sr-only">{tooltip}</span>
      </Button>
    );

    return (
      <Tooltip>
        <TooltipTrigger asChild>
          {disabled ? <span className="inline-flex">{button}</span> : button}
        </TooltipTrigger>
        <TooltipContent side={side} align={align}>
          {tooltip}
        </TooltipContent>
      </Tooltip>
    );
  },
);

TooltipIconButton.displayName = "TooltipIconButton";
