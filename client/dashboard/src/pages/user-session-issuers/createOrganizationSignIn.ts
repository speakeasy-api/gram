import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

export const SESSION_LENGTH_OPTIONS: ReadonlyArray<{
  hours: number;
  label: string;
}> = [
  { hours: 24, label: "1 day" },
  { hours: 24 * 7, label: "7 days" },
  { hours: 24 * 14, label: "14 days" },
  { hours: 24 * 30, label: "30 days" },
];

export const DEFAULT_SESSION_HOURS = 24 * 14;

/** Longest slug a sign-in can have. */
export const MAX_SLUG_LENGTH = 100;

function fullSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** The slug a name becomes: lowercase words joined by hyphens, capped in length. */
export function signInSlug(name: string): string {
  return fullSlug(name).slice(0, MAX_SLUG_LENGTH).replace(/-+$/, "");
}

export function signInNameError(
  name: string,
  organizationIssuers: Pick<UserSessionIssuer, "slug">[],
): string | null {
  if (name.trim() === "") return null;
  const full = fullSlug(name);
  if (full === "") return "Use letters or numbers";
  if (full.length > MAX_SLUG_LENGTH) return "Name is too long";
  const slug = signInSlug(name);
  return organizationIssuers.some((issuer) => issuer.slug === slug)
    ? "An organization-wide sign-in already has this name."
    : null;
}

/** "a", "a and b", "a, b and c". */
export function joinSignInNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}
