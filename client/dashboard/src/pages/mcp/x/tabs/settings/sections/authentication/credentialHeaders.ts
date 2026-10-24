import type { AuthClientHeader } from "@gram/client/models/components/authclientheader.js";
import type { AuthClientHeaderInput } from "@gram/client/models/components/authclientheaderinput.js";

/** One header an upstream credential sends, as it is being edited. */
export type HeaderDraft = {
  key: string;
  name: string;
  /** A stored value, or the caller's request header it is copied from. */
  source: "stored" | "forwarded";
  /** The new value; empty on a secret that keeps its stored one. */
  value: string;
  fromHeader: string;
  isSecret: boolean;
  /** The header already stores a secret, so leaving value empty keeps it. */
  keepsStored: boolean;
};

export function newHeaderDraft(
  source: HeaderDraft["source"],
  name = "",
): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name,
    source,
    value: "",
    fromHeader: "",
    isSecret: source === "stored",
    keepsStored: false,
  };
}

/** The drafts for a credential's saved headers, with no values revealed. */
export function headerDraftsFrom(headers: AuthClientHeader[]): HeaderDraft[] {
  return headers.map((header) => {
    const forwarded = !!header.valueFromRequestHeader;
    return {
      key: crypto.randomUUID(),
      name: header.name,
      source: forwarded ? "forwarded" : "stored",
      value: "",
      fromHeader: header.valueFromRequestHeader ?? "",
      isSecret: header.isSecret,
      keepsStored: header.isSecret && header.hasStoredValue,
    };
  });
}

export function headerInputs(drafts: HeaderDraft[]): AuthClientHeaderInput[] {
  return drafts.map((draft) => ({
    name: draft.name.trim(),
    isSecret: draft.source === "stored" && draft.isSecret,
    isRequired: draft.source === "forwarded",
    value: draft.source === "stored" && draft.value ? draft.value : undefined,
    valueFromRequestHeader:
      draft.source === "forwarded" ? draft.fromHeader.trim() : undefined,
  }));
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
    const key = name.toLowerCase().replaceAll("_", "-");
    if (names.has(key)) return `“${name}” is listed twice.`;
    names.add(key);
    if (generatesAuthorization(clientType) && key === "authorization") {
      return "Sign-in produces the Authorization header, so it cannot be set here.";
    }
    if (draft.source === "forwarded" && !draft.fromHeader.trim()) {
      return `Say which of the caller's headers “${name}” is copied from.`;
    }
    if (draft.source === "stored" && !draft.value && !draft.keepsStored) {
      return `Enter a value for “${name}”.`;
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
