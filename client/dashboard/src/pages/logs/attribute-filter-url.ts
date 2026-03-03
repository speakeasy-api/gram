import { Op } from "@gram/client/models/components/attributefilter";
import { VALUELESS_OPS, type ActiveAttributeFilter } from "./attribute-filter-types";

/**
 * Serialize active filters to a URL param value.
 * Format: `@user.region:eq:us-east-1,@env:exists`
 * Path and value are URI-encoded so commas/colons inside them survive the round-trip.
 * Returns null when there are no filters.
 */
export function serializeFilters(
  filters: ActiveAttributeFilter[],
): string | null {
  if (filters.length === 0) return null;
  return filters
    .map((f) => {
      const parts = [encodeURIComponent(f.path), f.op];
      if (f.value !== undefined) parts.push(encodeURIComponent(f.value));
      return parts.join(":");
    })
    .join(",");
}

const VALID_OPS = new Set<string>(Object.values(Op));

/**
 * Parse the `af` URL param back into ActiveAttributeFilter[].
 * Returns an empty array for null/empty input.
 */
export function parseFilters(param: string | null): ActiveAttributeFilter[] {
  if (!param) return [];

  return param
    .split(",")
    .map((segment) => {
      // Split into at most 3 parts: path, op, value (value may contain colons)
      const firstColon = segment.indexOf(":");
      if (firstColon === -1) return null;

      const path = decodeURIComponent(segment.slice(0, firstColon));
      const rest = segment.slice(firstColon + 1);

      const secondColon = rest.indexOf(":");
      let op: string;
      let value: string | undefined;

      if (secondColon === -1) {
        op = rest;
      } else {
        op = rest.slice(0, secondColon);
        value = decodeURIComponent(rest.slice(secondColon + 1));
      }

      if (!path || !VALID_OPS.has(op)) return null;
      // Ops that require a value (eq, not_eq, contains) must have one; reject
      // malformed segments like "attr:eq" that would produce an API error.
      if (!VALUELESS_OPS.includes(op as Op) && value === undefined) return null;

      return {
        id: crypto.randomUUID(),
        path,
        op: op as Op,
        value,
      };
    })
    .filter((f) => f !== null);
}
