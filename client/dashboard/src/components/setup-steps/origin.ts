// eslint-disable-next-line no-control-regex -- control characters are the point
const UNSAFE_IN_PATH = /[\\\x00-\x20\x7f]/;

/**
 * Whether src is a path on the dashboard's own origin. Only these are rendered:
 * a definition must not be able to point the operator's browser at an
 * arbitrary host. Browsers read a backslash as a slash and strip tabs and
 * newlines from URLs, so "/\host" and "/<tab>/host" would both leave the
 * origin; a backslash, whitespace or control character is refused anywhere.
 * The server's requireOriginPath applies the same rule.
 */
export function isRelativePath(src: string): boolean {
  return (
    src.startsWith("/") && !src.startsWith("//") && !UNSAFE_IN_PATH.test(src)
  );
}
