import { DENIED_PASS_THROUGH_SOURCES } from "@/lib/remote-identity/drafts/headerDrafts";
import type { AuthClientHeader } from "@gram/client/models/components/authclientheader.js";
import type { AuthClientHeaderInput } from "@gram/client/models/components/authclientheaderinput.js";

/** One header an upstream credential sends, as it is being edited. */
export type HeaderDraft = {
  key: string;
  name: string;
  description: string;
  /** A stored value, or the caller's request header it is copied from. */
  source: "stored" | "forwarded";
  /** The new value; empty on a header that keeps its stored one. */
  value: string;
  fromHeader: string;
  isSecret: boolean;
  /**
   * The name the header's stored value is saved under. The value is kept by
   * name, so leaving value empty keeps it only while the name still matches.
   */
  storedName: string | null;
};

/** A header name's identity: names match regardless of case, and '_' matches '-'. */
function headerKey(name: string): string {
  return name.trim().toLowerCase().replaceAll("_", "-");
}

export function newHeaderDraft(
  source: HeaderDraft["source"],
  name = "",
): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name,
    description: "",
    source,
    value: "",
    fromHeader: "",
    isSecret: source === "stored",
    storedName: null,
  };
}

/** The drafts for a credential's saved headers, with no values revealed. */
export function headerDraftsFrom(headers: AuthClientHeader[]): HeaderDraft[] {
  return headers.map((header) => {
    const forwarded = !!header.valueFromRequestHeader;
    return {
      key: crypto.randomUUID(),
      name: header.name,
      description: header.description ?? "",
      source: forwarded ? "forwarded" : "stored",
      value: "",
      fromHeader: header.valueFromRequestHeader ?? "",
      isSecret: header.isSecret,
      storedName: !forwarded && header.hasStoredValue ? header.name : null,
    };
  });
}

/** Whether leaving the value empty keeps the header's stored value. */
export function keepsStoredValue(draft: HeaderDraft): boolean {
  return (
    draft.source === "stored" &&
    draft.storedName !== null &&
    headerKey(draft.storedName) === headerKey(draft.name)
  );
}

export function headerInputs(drafts: HeaderDraft[]): AuthClientHeaderInput[] {
  return drafts.map((draft) => ({
    name: draft.name.trim(),
    description: draft.description.trim() || undefined,
    isSecret: draft.source === "stored" && draft.isSecret,
    isRequired: draft.source === "forwarded",
    value: draft.source === "stored" && draft.value ? draft.value : undefined,
    valueFromRequestHeader:
      draft.source === "forwarded" ? draft.fromHeader.trim() : undefined,
  }));
}

/**
 * Whether a header carries a Speakeasy credential, session, caller identity
 * or tunnel field. Such a header is never read from the caller's request,
 * and is never filled from it.
 */
function isProtectedInboundHeader(key: string): boolean {
  return (
    key === "x-speakeasy-identity" ||
    key.startsWith("gram-") ||
    key.startsWith("speakeasy-ai-") ||
    key.startsWith("x-gram-tunnel-") ||
    key === "proxy-authorization" ||
    key === "cookie" ||
    key === "set-cookie" ||
    key === "x-gram-agent-version"
  );
}

/** Whether a header belongs to the MCP protocol, which Speakeasy sets. */
function isMCPProtocolHeader(key: string): boolean {
  return (
    key === "mcp-protocol-version" ||
    key === "mcp-method" ||
    key === "mcp-name" ||
    (key.startsWith("mcp-param-") && key.length > "mcp-param-".length)
  );
}

/**
 * Why the drafts cannot be saved yet, in words for the administrator, or
 * null when they can.
 */
export function headerDraftProblem(
  clientType: string,
  drafts: HeaderDraft[],
): string | null {
  const names = new Set<string>();
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name) return "Every header needs a name.";
    const key = headerKey(name);
    if (names.has(key)) return `“${name}” is listed twice.`;
    names.add(key);
    if (generatesAuthorization(clientType) && key === "authorization") {
      return "Sign-in produces the Authorization header, so it cannot be set here.";
    }
    if (
      key === "set-cookie" ||
      key === "proxy-authorization" ||
      key === "x-speakeasy-identity" ||
      isMCPProtocolHeader(key)
    ) {
      return `“${name}” is reserved and cannot be sent upstream.`;
    }
    if (draft.source === "forwarded") {
      const from = draft.fromHeader.trim();
      if (!from) {
        return `Say which of the caller's headers “${name}” is copied from.`;
      }
      const fromKey = headerKey(from);
      if (
        DENIED_PASS_THROUGH_SOURCES.has(fromKey) ||
        isProtectedInboundHeader(fromKey)
      ) {
        return `The caller's “${from}” header is never forwarded upstream. Have callers send the value in a header of its own.`;
      }
      if (key === "cookie") {
        return "Cookie can only hold a stored value.";
      }
      if (isProtectedInboundHeader(key)) {
        return `“${name}” is a Speakeasy header, so it cannot be copied from the caller's request.`;
      }
    }
    if (draft.source === "stored" && !draft.value && !keepsStoredValue(draft)) {
      return draft.storedName === null
        ? `Enter a value for “${name}”.`
        : `Enter a value for “${name}”: the stored value stays with the name “${draft.storedName}”.`;
    }
  }
  if (
    (clientType === "static_credential" || clientType === "pass_through") &&
    drafts.length === 0
  ) {
    return "Add at least one header.";
  }
  return null;
}

/** Clients that obtain a token produce the Authorization header themselves. */
export function generatesAuthorization(clientType: string): boolean {
  return (
    clientType === "remote_session" ||
    clientType === "service_account" ||
    clientType === "ema"
  );
}
