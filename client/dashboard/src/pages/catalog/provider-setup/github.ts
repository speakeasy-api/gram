export function normalizeGitHubOrg(value: string): string {
  const trimmed = value.trim();
  const candidate = trimmed.replace(/^github\.com\//i, "https://github.com/");
  try {
    const url = new URL(candidate);
    if (
      url.hostname !== "github.com" ||
      url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password
    )
      return trimmed;
    const parts = url.pathname.split("/").filter(Boolean);
    if (parts[0] === "organizations" && parts[1] === "new") return trimmed;
    if (
      parts.length === 1 &&
      !["settings", "orgs", "organizations"].includes(parts[0]!)
    )
      return parts[0]!;
    if (["orgs", "organizations"].includes(parts[0] ?? "") && parts[1])
      return parts[1];
  } catch {
    // Plain organization names remain valid manual input.
  }
  return trimmed;
}
