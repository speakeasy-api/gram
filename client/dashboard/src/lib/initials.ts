/**
 * Canonical initials deriver for avatar fallbacks. Consolidates the many
 * one-off `getInitials`/`initialsOf` helpers scattered across the dashboard
 * (assistant-owner.tsx, InsightsEmployees.tsx, member-facepile.tsx,
 * challengeHelpers.ts, etc.) into a single implementation.
 *
 * - mode "name": first character of the first two words, uppercased.
 * - mode "email": first two characters of the local part (before "@"),
 *   uppercased.
 * - mode omitted: auto-detects — an identifier containing "@" is treated as
 *   an email, otherwise as a name.
 */
export function getInitials(
  identifier: string,
  mode?: "name" | "email",
): string {
  const trimmed = identifier.trim();
  if (!trimmed) return "";

  const resolvedMode = mode ?? (trimmed.includes("@") ? "email" : "name");

  if (resolvedMode === "email") {
    const local = trimmed.split("@")[0] ?? trimmed;
    return local.slice(0, 2).toUpperCase();
  }

  const words = trimmed.split(/\s+/).filter(Boolean);
  return words
    .slice(0, 2)
    .map((word) => word[0] ?? "")
    .join("")
    .toUpperCase();
}
