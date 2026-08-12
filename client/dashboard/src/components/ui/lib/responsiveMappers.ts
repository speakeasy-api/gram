import {
  Alignment,
  Gap,
  Padding,
  PaddingPerSide,
} from "@/components/ui/lib/types";
import {
  isPaddingHorizontalOrVerticalAxis,
  isPaddingPerSide,
  isPaddingPerSideValue,
} from "./typeUtils";

const directionClasses: Record<"horizontal" | "vertical", string> = {
  horizontal: "flex-row",
  vertical: "flex-col",
};

export const directionToFlexMapper = (
  direction: "horizontal" | "vertical",
): string => directionClasses[direction];
export const gapMapper = (gap: Gap): string => `gap-${gap}`;

const paddingPerSideMapper = (padding: PaddingPerSide): string => {
  if (isPaddingHorizontalOrVerticalAxis(padding)) {
    const { x, y } = padding;
    return `px-${x} py-${y}`;
  }

  if (isPaddingPerSideValue(padding)) {
    const { top, right, bottom, left } = padding;
    return `pt-${top} pr-${right} pb-${bottom} pl-${left}`;
  }

  return "";
};

export const paddingMapper = (padding: Padding): string => {
  if (isPaddingPerSide(padding)) return paddingPerSideMapper(padding);
  return `p-${padding}`;
};
export const colSpanMapper = (colSpan: number): string => `col-span-${colSpan}`;

const alignmentClasses: Record<Alignment, string> = {
  start: "items-start",
  center: "items-center",
  end: "items-end",
  stretch: "items-stretch",
  baseline: "items-baseline",
};

export const alignmentMapper = (alignment: Alignment): string =>
  alignmentClasses[alignment];

const justifyClasses: Record<
  "start" | "center" | "end" | "space-between" | "space-evenly",
  string
> = {
  start: "justify-start",
  center: "justify-center",
  end: "justify-end",
  "space-between": "justify-between",
  "space-evenly": "justify-evenly",
};

export const justifyMapper = (justify: keyof typeof justifyClasses): string =>
  justifyClasses[justify];

const wrapClasses: Record<"nowrap" | "wrap" | "wrap-reverse", string> = {
  nowrap: "flex-nowrap",
  wrap: "flex-wrap",
  "wrap-reverse": "flex-wrap-reverse",
};

export const wrapMapper = (wrap: "nowrap" | "wrap" | "wrap-reverse"): string =>
  wrapClasses[wrap];
