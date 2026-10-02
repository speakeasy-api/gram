// Mirrors what the server refuses on the write path, so the reason appears next
// to the field instead of arriving as a toast after submit. Deliberately not a
// full URL validator: the server stays the authority, this is the early warning.
export function httpsUrlProblem(raw: string, isIssuer: boolean): string | null {
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return null;
  }

  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a complete URL, including https://.";
  }

  if (parsed.protocol !== "https:") {
    return "Must use https. Gram fetches the signing keys over this URL, so http would put key retrieval in the clear.";
  }
  // The browser repairs "https:host" and "https:/host" into a URL with a host;
  // the server parses the raw string and finds none. Require the authority as
  // written.
  if (!/^https:\/\/[^/?#]/i.test(trimmed)) {
    return "Enter a complete URL, including https://.";
  }
  const host = parsed.hostname.replace(/\.$/, "");
  if (!host.includes(".") || /^[\d.]+$/.test(host)) {
    return "Must name a fully qualified domain, not an IP address or a single-label host.";
  }
  // The server refuses these for both URLs. The raw string is checked rather
  // than the parsed fields, which report nothing for a bare "?" or "#", or for
  // an empty userinfo such as "https://@host" — the server still refuses that,
  // because Go's parser records any "@" in the authority as userinfo.
  const authority =
    trimmed.replace(/^[a-z][a-z0-9+.-]*:\/\//i, "").split(/[/?#]/)[0] ?? "";
  if (authority.includes("@")) {
    return "Must not carry a username or password.";
  }
  if (trimmed.includes("?") || trimmed.includes("#")) {
    return isIssuer
      ? "An issuer identifier carries no query string or fragment."
      : "Must not carry a query string or fragment.";
  }

  return null;
}
