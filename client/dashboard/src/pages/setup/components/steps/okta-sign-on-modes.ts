/**
 * Okta's own names for how an application signs people in. An unmapped mode is
 * shown as Okta wrote it rather than guessed at — a wrong friendly name would
 * be worse than an unfamiliar one.
 */
const SIGN_ON_MODES: Record<string, string> = {
  OPENID_CONNECT: "OpenID Connect",
  SAML_2_0: "SAML",
  BROWSER_PLUGIN: "Browser plugin",
  BOOKMARK: "Bookmark",
  AUTO_LOGIN: "Auto login",
  WS_FEDERATION: "WS-Federation",
};

export function signOnModeLabel(mode: string | undefined): string {
  if (!mode) return "—";
  return SIGN_ON_MODES[mode] ?? mode;
}
