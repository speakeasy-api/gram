import { useLocation } from "react-router";

type Parser = {
  parse: (value: string) => string | null;
  defaultValue?: string;
};

/** Test stand-in for nuqs `useQueryState`: the real adapter reads window.location, not MemoryRouter. */
export function useRouterQueryState(
  key: string,
  parser?: Parser,
): [string | null] {
  const value = new URLSearchParams(useLocation().search).get(key);
  if (!parser) return [value];
  return [(value ? parser.parse(value) : null) ?? parser.defaultValue ?? null];
}
