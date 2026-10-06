/**
 * Whether src is a path on the dashboard's own origin. Only these are rendered:
 * a definition must not be able to point the operator's browser at an
 * arbitrary host.
 */
export function isRelativePath(src: string): boolean {
  return src.startsWith("/") && !src.startsWith("//");
}
