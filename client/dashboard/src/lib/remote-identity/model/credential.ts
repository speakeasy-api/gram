import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import {
  REDACTED_SECRET,
  UNCHANGED_SECRET,
  secretHasValue,
  secretToWireValue,
  type Secret,
} from "./secret";

/**
 * How the shared static credential is assembled. Client Credentials is
 * advertised but not built: it appears in the UI as a roadmap item, never as a
 * selectable state, so nothing here knows how to encode it.
 */
export type AgentCredentialFormat =
  | "bearer"
  | "basic"
  | "manual"
  | "client-credentials";

/**
 * The Agent Identity credential: one value every caller of a server shares,
 * sent upstream as Authorization.
 *
 * Kept as the parts rather than the assembled string so the form can show a
 * preview of exactly what the upstream receives, and so a rotated token does
 * not have to be re-derived from a header nobody can read back.
 */
export type AgentCredential = {
  readonly format: AgentCredentialFormat;
  readonly prefix: string;
  readonly token: Secret;
  readonly username: string;
  readonly password: Secret;
  /** The whole header value, for a scheme we do not model. */
  readonly raw: Secret;
};

export const EMPTY_AGENT_CREDENTIAL: AgentCredential = {
  format: "bearer",
  prefix: "Bearer",
  token: UNCHANGED_SECRET,
  username: "",
  password: UNCHANGED_SECRET,
  raw: UNCHANGED_SECRET,
};

/**
 * Which format a saved header was written in.
 *
 * Nothing configured yet starts on Bearer — what almost every upstream wants —
 * rather than on Manual, which would ask an operator to hand-assemble a header
 * before they have seen the simpler options. A redacted value tells us nothing
 * about its scheme, so it gets the same treatment.
 */
export function credentialFormatFromHeader(
  header: RemoteMcpServerHeader | undefined,
): AgentCredentialFormat {
  if (!header) return "bearer";
  const value = header.value ?? "";
  if (value.startsWith("Basic ")) return "basic";
  if (value.startsWith("Bearer ") || value === REDACTED_SECRET) return "bearer";
  return "manual";
}

export function encodeBasicCredential(
  username: string,
  password: string,
): string {
  const bytes = new TextEncoder().encode(`${username}:${password}`);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/** Read a saved Authorization header back into the parts a form can edit. */
export function credentialFromHeader(
  header: RemoteMcpServerHeader | undefined,
): AgentCredential {
  const format = credentialFormatFromHeader(header);
  const value = header?.value ?? "";
  const readable = value !== REDACTED_SECRET;

  return {
    ...EMPTY_AGENT_CREDENTIAL,
    format,
    token:
      readable && value.startsWith("Bearer ")
        ? { kind: "set", value: value.slice("Bearer ".length) }
        : UNCHANGED_SECRET,
    raw:
      format === "manual" && readable
        ? { kind: "set", value }
        : UNCHANGED_SECRET,
  };
}

/**
 * The exact Authorization value to send, or null when the credential is not
 * complete enough to send at all. Null is not an error — it is the resting
 * state of a form nobody has filled in yet.
 */
export function credentialToAuthorizationValue(
  credential: AgentCredential,
): string | null {
  const { format, prefix } = credential;
  if (format === "client-credentials") return null;

  if (format === "bearer") {
    const token = secretToWireValue(credential.token);
    if (!token) return null;
    const scheme = prefix.trim();
    return scheme ? `${scheme} ${token}` : token;
  }

  if (format === "basic") {
    const password = secretToWireValue(credential.password);
    if (!credential.username || !password) return null;
    return `Basic ${encodeBasicCredential(credential.username, password)}`;
  }

  const raw = secretToWireValue(credential.raw);
  return raw ? raw : null;
}

/** Bullets, capped so a long token cannot wrap a preview into a paragraph. */
function maskSecretValue(value: string): string {
  return value ? "•".repeat(Math.min(value.length, 28)) : "";
}

/**
 * How the header reads on screen. Shows the shape of what will be sent even
 * before anything is entered, so the operator can see where their value lands
 * rather than staring at an empty box.
 */
export function credentialPreview(
  credential: AgentCredential,
  reveal: boolean,
): string {
  const show = (secret: Secret, placeholder: string): string => {
    if (!secretHasValue(secret)) return placeholder;
    const value = secretToWireValue(secret) ?? "";
    return reveal ? value : maskSecretValue(value);
  };

  if (credential.format === "bearer") {
    const shown = show(credential.token, "<token>");
    const scheme = credential.prefix.trim();
    return scheme ? `${scheme} ${shown}` : shown;
  }

  if (credential.format === "basic") {
    const password = secretToWireValue(credential.password) ?? "";
    if (!credential.username && !password) {
      return "Basic <base64(username:password)>";
    }
    const encoded = encodeBasicCredential(credential.username, password);
    return `Basic ${reveal ? encoded : maskSecretValue(encoded)}`;
  }

  return show(credential.raw, "<header value>");
}
