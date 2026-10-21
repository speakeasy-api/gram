export function attachmentTypeLabel(type: string): string {
  const normalized = type.replace(/[-_]+/g, " ").trim();
  if (!normalized) return "File";

  return normalized.charAt(0).toUpperCase() + normalized.slice(1);
}

/** Raster image types; SVG is excluded because it is a document that can carry script. */
const PASSIVE_IMAGE_TYPES = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/avif",
  "image/bmp",
]);

/**
 * Non-media types a browser displays without interpreting markup or running
 * script. PDF is included because browsers render it in an isolated viewer.
 */
const PASSIVE_DOCUMENT_TYPES = new Set([
  "text/plain",
  "text/csv",
  "text/markdown",
  "application/json",
  "application/pdf",
]);

function mediaType(contentType: string): string {
  return (contentType.split(";")[0] ?? "").trim().toLowerCase();
}

/** Whether an image attachment can be previewed from a blob URL. */
export function isPassiveImageType(contentType: string): boolean {
  return PASSIVE_IMAGE_TYPES.has(mediaType(contentType));
}

/**
 * Whether an attachment of this type may be opened in a new tab from a blob
 * URL. A blob URL takes the dashboard's origin and none of the serve
 * endpoint's sandboxing headers, so HTML, SVG or XML opened that way would run
 * script as the dashboard. Every other type is downloaded instead.
 */
export function opensInBrowserTab(contentType: string): boolean {
  const type = mediaType(contentType);
  return (
    type.startsWith("audio/") ||
    PASSIVE_IMAGE_TYPES.has(type) ||
    PASSIVE_DOCUMENT_TYPES.has(type)
  );
}
