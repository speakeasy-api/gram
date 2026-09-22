export function headerValue(
  headers: Record<string, string[]>,
  name: string,
): string | undefined {
  const wanted = name.toLowerCase();
  const key = Object.keys(headers).find((k) => k.toLowerCase() === wanted);
  return key ? headers[key]?.[0] : undefined;
}

export function filenameFromContentDisposition(
  header: string | null | undefined,
  fallback: string,
): string {
  if (!header) return fallback;
  const extended = /(?:^|;\s*)filename\*=([^']*)'[^']*'([^;]+)/i.exec(header);
  if (extended?.[2] && extended[1]?.toLowerCase() === "utf-8") {
    try {
      return decodeURIComponent(extended[2].trim());
    } catch {
      return fallback;
    }
  }
  const plain = /(?:^|;\s*)filename=(?:"([^"]*)"|([^;]+))/i.exec(header);
  const name = (plain?.[1] ?? plain?.[2])?.trim();
  return name ? name : fallback;
}

/** Hand a blob to the browser as a file download; revoked a task later so async downloads still read it. */
export function downloadBlob(blob: Blob, filename: string): void {
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
}
