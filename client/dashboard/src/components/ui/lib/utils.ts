export { cn } from "@/lib/utils";

/** `fooBar` -> `foo-bar`. Used to derive lucide's per-icon class names. */
export const toKebabCase = (value: string): string =>
  value.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
