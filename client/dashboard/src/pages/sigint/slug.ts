export function slugError(value: string, editing: boolean): string | null {
  const slug = value.trim();
  if (!slug && !editing) return null;
  if (!/^[a-z0-9_-]{1,40}$/.test(slug)) {
    return "Use 1–40 lowercase letters, numbers, hyphens, or underscores.";
  }
  return null;
}
