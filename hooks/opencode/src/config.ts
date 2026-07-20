// Config is env-only. NEVER hardcode a key or URL here — a hardcoded key in
// the throwaway demo this package replaces was a security incident.

export interface GramConfig {
  url: string;
  key?: string;
  project?: string;
}

let warned = false;

export function loadConfig(): GramConfig {
  const url = (process.env.GRAM_URL ?? "https://app.getgram.ai").replace(
    /\/+$/,
    "",
  );
  const key = process.env.GRAM_KEY;
  const project = process.env.GRAM_PROJECT;

  if (!warned && (!key || !project)) {
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
