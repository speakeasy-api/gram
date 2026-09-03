const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

export function getAdminServerUrl(
  value: string | null | undefined,
  isDev: boolean,
): string | null {
  if (!value) return null;

  try {
    const url = new URL(value);
    if (url.protocol === "https:") return value;
    if (isDev && url.protocol === "http:" && LOOPBACK_HOSTS.has(url.hostname)) {
      return value;
    }
  } catch {
    // Invalid URLs fail closed.
  }

  return null;
}

export function replaceAdminServerUrl(html: string, value: string): string {
  const escaped = value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
  return html.replace("${GRAM_ADMIN_SERVER_URL}", () => escaped);
}
