// RFC 6266 filename: the RFC 5987 `filename*=UTF-8''...` form wins over the
// plain quoted `filename="..."` form; a malformed percent-encoding falls back
// to the plain form rather than throwing.
export function filenameFromDisposition(
  disposition: string | null,
): string | undefined {
  if (!disposition) return undefined;
  const extended = /filename\*\s*=\s*(?:UTF-8|utf-8)'[^']*'([^;]+)/.exec(
    disposition,
  );
  if (extended?.[1]) {
    try {
      return decodeURIComponent(extended[1].trim());
    } catch {
      // fall through to the plain form
    }
  }
  return /filename\s*=\s*"([^"]+)"/.exec(disposition)?.[1];
}
