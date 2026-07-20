// Config is env-only. NEVER hardcode a key or URL here — a hardcoded key in
// the throwaway demo this package replaces was a security incident.

export interface GramConfig {
  url: string;
  key?: string;
  project?: string;
}

// A GRAM_URL is safe to send to only over TLS, or over http to a loopback
// host for local dev. Anything else would transmit the Gram-Key header and
// event payloads in plaintext.
export function isSecureUrl(url: string): boolean {
  try {
    const { protocol, hostname } = new URL(url);
    if (protocol === "https:") return true;
    return (
      protocol === "http:" &&
      (hostname === "localhost" ||
        hostname === "127.0.0.1" ||
        hostname === "::1" ||
        hostname === "[::1]")
    );
  } catch {
    return false;
  }
}

let warned = false;

export function loadConfig(): GramConfig {
  const url = (process.env.GRAM_URL ?? "https://app.getgram.ai").replace(
    /\/+$/,
    "",
  );
  const key = process.env.GRAM_KEY;
  const project = process.env.GRAM_PROJECT;

  if (!warned && !isSecureUrl(url)) {
    warned = true;
    console.warn(
      "[gram-opencode-observability] GRAM_URL " +
        url +
        " is not a TLS (https) endpoint; refusing to send events to avoid " +
        "leaking GRAM_KEY and event payloads in plaintext. Use an https URL.",
    );
  } else if (!warned && (!key || !project)) {
    warned = true;
    console.warn(
      "[gram-opencode-observability] GRAM_KEY and/or GRAM_PROJECT are not set. " +
        "Events will be sent unauthenticated to " +
        url +
        " and dropped server-side. Set both env vars to enable observability. " +
        "See hooks/opencode/README.md.",
    );
  }

  return { url, key, project };
}
