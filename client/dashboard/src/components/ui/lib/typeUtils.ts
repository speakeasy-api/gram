import {
  Breakpoint,
  breakpoints,
  Direction,
  PaddingPerAxis,
  PaddingPerSide,
  PaddingPerSides,
  ResponsiveValue,
  Size,
  sizes,
} from "@/components/ui/lib/types";
import { Group } from "@/components/ui/Table";

/**
 * Create a range of numbers from 0 to N
 * @example
 * type Range0to100 = Range<100> // [0, 1, 2, ..., 100]
 */
export type Range<
  N extends number,
  Arr extends unknown[] = [],
> = Arr["length"] extends N
  ? [...Arr, N][number]
  : Range<N, [...Arr, Arr["length"]]>;

export function isResponsiveValueObject<T>(
  value: unknown,
): value is ResponsiveValue<T> & Record<Breakpoint, T> {
  return (
    typeof value === "object" &&
    value !== null &&
    Object.keys(value).every((key) => isBreakpoint(key))
  );
}

export function isSize(value: unknown): value is Size {
  return (
    typeof value === "string" && (sizes as readonly string[]).includes(value)
  );
}

export function isDirection(value: unknown): value is Direction {
  return (
    typeof value === "string" &&
    (["row", "column"] as readonly string[]).includes(value)
  );
}

/**
 * Checks if the value is an object with x and y properties
 */
export function isPaddingHorizontalOrVerticalAxis(
  value: unknown,
): value is PaddingPerAxis {
  return (
    typeof value === "object" &&
    value !== null &&
    // x or y must be present
    ("x" in value || "y" in value)
  );
}

export function isPaddingPerSideValue(
  value: unknown,
): value is PaddingPerSides {
  return (
    typeof value === "object" &&
    value !== null &&
    ("top" in value || "right" in value || "bottom" in value || "left" in value)
  );
}

export function isPaddingPerSide(value: unknown): value is PaddingPerSide {
  return isPaddingHorizontalOrVerticalAxis(value) || isPaddingPerSides(value);
}

export function isPaddingPerSides(value: unknown): value is PaddingPerSides {
  return (
    typeof value === "object" &&
    value !== null &&
    "top" in value &&
    "right" in value &&
    "bottom" in value &&
    "left" in value
  );
}

/**
 * Asserts that a condition is true, otherwise throws an error
 * Can be used to narrow types
 */
export function assert(condition: unknown, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}

function isBreakpoint(key: string): key is Breakpoint {
  return (breakpoints as readonly string[]).includes(key);
}

export function isGroupOf<T extends object>(data: unknown): data is Group<T> {
  return (
    typeof data === "object" &&
    data !== null &&
    "key" in data &&
    "items" in data
  );
}
