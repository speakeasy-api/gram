import React from "react";
import { cn } from "@/lib/utils";
import { getResponsiveClasses } from "@/components/ui/lib/utils";
import { Gap, Padding, ResponsiveValue } from "@/components/ui/lib/types";
import {
  gapMapper,
  paddingMapper,
  wrapMapper,
} from "@/components/ui/lib/responsiveMappers";

type StackDirection = "horizontal" | "vertical";
type StackAlign = "stretch" | "start" | "center" | "end" | "baseline";
type StackJustify =
  | "start"
  | "center"
  | "end"
  | "space-between"
  | "space-evenly";
type StackWrap = "wrap" | "nowrap";
type TailwindJustify =
  | "justify-start"
  | "justify-center"
  | "justify-end"
  | "justify-between"
  | "justify-evenly";
type TailwindAlign =
  | "items-start"
  | "items-center"
  | "items-end"
  | "items-baseline"
  | "items-stretch";
type TailwindDirection = "flex-row" | "flex-col";

export interface StackProps {
  children: React.ReactNode;

  /** Specify the orientation for the stack container */
  direction?: ResponsiveValue<StackDirection>;

  /** Specify the gap between children elements in the stack */
  gap?: ResponsiveValue<Gap>;

  /** Specify the padding of the stack container */
  padding?: ResponsiveValue<Padding>;

  /** Specify the alignment between items in the cross-axis of the orientation */
  align?: ResponsiveValue<StackAlign>;

  /** Specify how items will be distributed in the stacking direction */
  justify?: ResponsiveValue<StackJustify>;

  /** Specify whether items are forced onto one line or can wrap */
  wrap?: ResponsiveValue<StackWrap>;

  className?: string;
}

export function Stack({
  children,
  direction = "vertical",
  gap = 0,
  padding = 0,
  align = "stretch",
  justify = "start",
  wrap = "nowrap",
  className,
}: StackProps): React.JSX.Element {
  const alignMapper = (val: StackAlign): TailwindAlign => {
    switch (val) {
      case "start":
        return "items-start";
      case "center":
        return "items-center";
      case "end":
        return "items-end";
      case "baseline":
        return "items-baseline";
      case "stretch":
        return "items-stretch";
      default:
        return "items-stretch";
    }
  };

  const justifyMapper = (val: StackJustify): TailwindJustify => {
    switch (val) {
      case "start":
        return "justify-start";
      case "center":
        return "justify-center";
      case "end":
        return "justify-end";
      case "space-between":
        return "justify-between";
      case "space-evenly":
        return "justify-evenly";
      default:
        return "justify-start";
    }
  };

  const directionToFlexMapper = (val: StackDirection): TailwindDirection => {
    return val === "horizontal" ? "flex-row" : "flex-col";
  };

  return (
    <div
      className={cn(
        "flex",
        getResponsiveClasses(direction, directionToFlexMapper),
        getResponsiveClasses(gap, gapMapper),
        getResponsiveClasses(padding, paddingMapper),
        getResponsiveClasses(wrap, wrapMapper),
        getResponsiveClasses(align, alignMapper),
        getResponsiveClasses(justify, justifyMapper),
        className,
      )}
    >
      {children}
    </div>
  );
}

interface StackItemProps {
  children: React.ReactNode;
  /** Allow item to keep size or expand to fill the available space */
  grow?: ResponsiveValue<boolean>;
  className?: string;
}

Stack.Item = function StackItem({ children, grow, className }: StackItemProps) {
  const growMapper = (val: boolean) => (val ? "flex-1" : "flex-initial");
  return (
    <div
      className={cn(getResponsiveClasses(grow ?? false, growMapper), className)}
    >
      {children}
    </div>
  );
};
