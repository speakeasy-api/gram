export function attachmentTypeLabel(type: string): string {
  const normalized = type.replace(/[-_]+/g, " ").trim();
  if (!normalized) return "File";

  return normalized.charAt(0).toUpperCase() + normalized.slice(1);
}
