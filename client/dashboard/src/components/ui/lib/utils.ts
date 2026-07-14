import { Breakpoint, ResponsiveValue } from "@/components/ui/lib/types";
import { isResponsiveValueObject } from "@/components/ui/lib/typeUtils";

/**
 * Given an object of responsive values for T and a mapper function, return a string of class names
 * that correspond to the responsive values.
 *
 * @param value - The responsive value
 * @param mapper - A function that takes the value and returns a string of class names
 * @returns A string of class names
 * @example
 * const gapMapper = (gap: number) => `gap-${gap}`
 * const gap = getResponsiveClasses({ sm: 0, md: 10 }, gapMapper)
 * // => 'gap-0 md:gap-10'
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
        const classFragments = resolvedClasses.split(" ");

        return classFragments
          .map((fragment) => {
            return breakpoint === "xs" ? fragment : `${breakpoint}:${fragment}`;
          })
          .join(" ");
      })
      .join(" ");
  }
  return mapper(value as T, "xs");
}

/**
 * Converts string to kebab case
 *
 * @param {string} string
 * @returns {string} A kebabized string
 */
export const toKebabCase = (string: string): string =>
  string.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
