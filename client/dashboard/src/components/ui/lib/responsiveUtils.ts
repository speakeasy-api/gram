import {
  Breakpoint,
  breakpoints,
  Gap,
  ResponsiveValue,
  Size,
} from "@/components/ui/lib/types";
import { isResponsiveValueObject, isSize } from "./typeUtils";

export const gapMapper = (gap: Gap): string => `gap-${gap}`;

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
    const resolved = breakpoint ? size[breakpoint] : undefined;
    if (resolved) return resolved;
  }

  return fallback;
};

/**
 * Given an object of responsive values for T and a mapper function, return a
 * string of class names that correspond to the responsive values.
 *
 * @example
 * const gap = getResponsiveClasses({ sm: 0, md: 10 }, (g) => `gap-${g}`)
 * // => "gap-0 md:gap-10"
 */
export function getResponsiveClasses<T>(
  value: ResponsiveValue<T>,
  mapper: (val: T, breakpoint: Breakpoint) => string,
): string {
  if (isResponsiveValueObject(value)) {
    return Object.entries(value)
      .filter(([, val]) => val !== undefined)
      .map(([breakpoint, val]) => {
        const resolvedClasses = mapper(val as T, breakpoint as Breakpoint);

        return resolvedClasses
          .split(" ")
          .map((fragment) =>
            breakpoint === "xs" ? fragment : `${breakpoint}:${fragment}`,
          )
          .join(" ");
      })
      .join(" ");
  }
  return mapper(value as T, "xs");
}
