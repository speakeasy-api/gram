import { Gap, Padding, PaddingPerSide } from "@/components/ui/lib/types";
import {
  isPaddingHorizontalOrVerticalAxis,
  isPaddingPerSide,
  isPaddingPerSideValue,
} from "./typeUtils";

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

const wrapClasses: Record<"nowrap" | "wrap" | "wrap-reverse", string> = {
  nowrap: "flex-nowrap",
  wrap: "flex-wrap",
  "wrap-reverse": "flex-wrap-reverse",
};

export const wrapMapper = (wrap: "nowrap" | "wrap" | "wrap-reverse"): string =>
  wrapClasses[wrap];
