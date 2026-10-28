const MAX_ADOPTION_DASHBOARD_URL_LENGTH = 2048;

// Mirrors the server: empty clears the link, anything else must be https.
export function adoptionDashboardUrlProblem(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed === "") {
    return null;
  }
  if (trimmed.length > MAX_ADOPTION_DASHBOARD_URL_LENGTH) {
    return "The link must be at most 2048 characters.";
  }
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a full https:// link.";
  }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password) {
    return "Enter a full https:// link.";
  }
  return null;
}
