import {
  Breakpoint,
  breakpoints,
  ResponsiveValue,
  Size,
} from "@/components/ui/lib/types";
import { isResponsiveValueObject, isSize } from "./typeUtils";

export const resolveSizeForBreakpoint = (
  currentBreakpoint: Breakpoint,
  size: ResponsiveValue<Size>,
  fallback: Size = "medium",
): Size => {
  if (!isResponsiveValueObject<Size>(size)) {
    return isSize(size) ? size : fallback;
  }

  const currentBreakpointIndex = breakpoints.indexOf(currentBreakpoint);

  for (let i = currentBreakpointIndex; i >= 0; i--) {
    const breakpoint = breakpoints[i];
    if (breakpoint && size[breakpoint]) return size[breakpoint];
  }

  return fallback;
};
