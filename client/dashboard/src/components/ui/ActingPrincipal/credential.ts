import type { Principal } from "./text";

/**
 * How Speakeasy signed an agent's call in to the upstream server: the auth
 * client that served it, and for a personal connection whose it was. Read off
 * a tool-call row; rows recorded before this was tracked carry none.
 */
export type UpstreamCredential = {
  /** The auth client's type, such as `remote_session` or `service_account`. */
  type: string;
  clientId?: string | undefined;
  /** The person whose own connection served the call. */
  ownerUserId?: string | undefined;
  /** Their name, when it resolved to someone in the organization. */
  ownerName?: string | undefined;
};

const PERSONAL_CONNECTION = "remote_session";

/** What each auth client type is called where a person reads it. */
const TYPE_WORDS: Record<string, string> = {
  remote_session: "Personal connection",
  service_account: "Service account",
  static_credential: "Static credential",
  pass_through: "Passed-through credential",
  ema: "Enterprise sign-in",
};

/**
 * The credential in a few words, said from the reader's side: "your
 * connection (Ada Lovelace)", "Alex Morgan's connection", "shared credential".
 * Undefined for a type this dashboard does not know, so the caller keeps the
 * rendering it had before credentials were tracked.
 */
export function upstreamCredentialPhrase(
  credential: UpstreamCredential,
  viewerUserId?: string,
): string | undefined {
  switch (credential.type) {
    case PERSONAL_CONNECTION: {
      const { ownerUserId, ownerName } = credential;
      if (ownerUserId && viewerUserId && ownerUserId === viewerUserId) {
        return ownerName ? `your connection (${ownerName})` : "your connection";
      }
      return ownerName
        ? `${ownerName}'s connection`
        : "a teammate's connection";
    }
    case "service_account":
    case "static_credential":
      return "shared credential";
    case "pass_through":
      return "passed-through credential";
    case "ema":
      return "enterprise sign-in";
    default:
      return undefined;
  }
}

/**
 * The raw values behind the phrase, for a tooltip: the auth client's type in
 * words and its id, so whoever is debugging a call can find the client.
 */
export function upstreamCredentialDetail(
  credential: UpstreamCredential,
): string {
  const parts = [
    `Signed in with: ${TYPE_WORDS[credential.type] ?? credential.type}`,
  ];
  if (credential.clientId) parts.push(`Auth client ${credential.clientId}`);
  return parts.join(" · ");
}

/**
 * `ActingPrincipal`'s `signedInWith` for a row's credential, or undefined
 * when the row recorded none (or one of a type this dashboard does not know),
 * so the row keeps the "on behalf of" rendering it had before.
 */
export function signedInWithFor(
  credential: UpstreamCredential | undefined,
  viewerUserId?: string,
): { label: string; detail: string; person?: Principal } | undefined {
  if (!credential) return undefined;
  const label = upstreamCredentialPhrase(credential, viewerUserId);
  if (!label) return undefined;
  const detail = upstreamCredentialDetail(credential);
  if (credential.type !== PERSONAL_CONNECTION || !credential.ownerUserId) {
    return { label, detail };
  }
  const person: Principal = { id: credential.ownerUserId };
  if (credential.ownerName) person.name = credential.ownerName;
  return { label, detail, person };
}
