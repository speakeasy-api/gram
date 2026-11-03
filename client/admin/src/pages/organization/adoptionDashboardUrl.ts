const MAX_ADOPTION_DASHBOARD_URL_LENGTH = 2048;

const HTTPS_PREFIX = /^https:\/\//i;

// Follows the server's rules so most mistakes show inline before a request:
// empty clears the link, anything else must be an https URL with a host. The
// limit counts characters (code points) like the server. Browser and Go URL
// parsers differ on rare inputs, so the server stays the authority.
export function adoptionDashboardUrlProblem(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed === "") {
    return null;
  }
  if (Array.from(trimmed).length > MAX_ADOPTION_DASHBOARD_URL_LENGTH) {
    return "The link must be at most 2048 characters.";
  }
  // new URL accepts "https:example.com" and "https:/example.com", which the
  // server refuses. With the prefix, new URL also throws when the host is empty.
  if (!HTTPS_PREFIX.test(trimmed)) {
    return "Enter a full https:// link.";
  }
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a full https:// link.";
  }
  if (parsed.username || parsed.password) {
    return "Enter a full https:// link.";
  }
  return null;
}
