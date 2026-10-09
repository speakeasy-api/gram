import type { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import {
  authorizationHeaderGuard,
  remoteHeaderPolicyIssue,
  remoteHeaderPolicyReasonMessage,
  type RemoteHeaderPolicyIssue,
} from "../model/headers";
import type { IdentityMode } from "../model/identity";
import { REDACTED_SECRET } from "../model/secret";

/**
 * One editable header row, and the rules for comparing, validating and
 * writing them.
 *
 * Canonical on purpose: the Authorization rules in particular are the identity
 * choice reaching into the header list, and they belong somewhere both halves
 * can see rather than inside whichever component happened to need them first.
 */
export type HeaderSource = "static" | "request";

export type HeaderDraft = {
  key: string;
  /** Set for headers that already exist on the server. */
  id?: string;
  name: string;
  source: HeaderSource;
  staticValue: string;
  valueFromRequestHeader: string;
  isRequired: boolean;
  isSecret: boolean;
  hadSecret: boolean;
  /** Row was seeded from the endpoint's catalog entry (and is unsaved). */
  fromCatalog?: boolean;
  /**
   * The editable fields as the server last returned them, for a saved row.
   * A saved row the header policy now refuses stays as it is until the
   * operator edits it, so it never blocks saving the other rows.
   */
  saved?: SavedHeaderFields;
};

/** A saved row's editable fields, compared to tell an untouched row apart. */
type SavedHeaderFields = Pick<
  HeaderDraft,
  | "name"
  | "source"
  | "staticValue"
  | "valueFromRequestHeader"
  | "isRequired"
  | "isSecret"
>;

function headerSourceFromServer(header: RemoteMcpServerHeader): HeaderSource {
  if (header.valueFromRequestHeader) {
    return "request";
  }
  return "static";
}

export function headerDraftFromServer(
  header: RemoteMcpServerHeader,
): HeaderDraft {
  const source = headerSourceFromServer(header);
  const isRedactedSecret = header.isSecret && header.value === REDACTED_SECRET;

  const fields: SavedHeaderFields = {
    name: header.name,
    source,
    staticValue:
      source === "static"
        ? isRedactedSecret
          ? REDACTED_SECRET
          : (header.value ?? "")
        : "",
    valueFromRequestHeader: header.valueFromRequestHeader ?? "",
    isRequired: header.isRequired,
    isSecret: header.isSecret,
  };

  return {
    key: header.id,
    id: header.id,
    ...fields,
    hadSecret: header.isSecret,
    saved: fields,
  };
}

/** Whether a saved row still holds exactly what the server returned. */
function isUnchangedSavedDraft(draft: HeaderDraft): boolean {
  const saved = draft.saved;
  if (!draft.id || !saved) return false;
  return (
    draft.name === saved.name &&
    draft.source === saved.source &&
    draft.staticValue === saved.staticValue &&
    draft.valueFromRequestHeader === saved.valueFromRequestHeader &&
    draft.isRequired === saved.isRequired &&
    draft.isSecret === saved.isSecret
  );
}

/**
 * The header policy's verdict on the row the server holds, for warning about
 * a saved row that predates the policy. Null for an unsaved row.
 */
export function savedHeaderPolicyIssue(
  draft: HeaderDraft,
): RemoteHeaderPolicyIssue | null {
  const saved = draft.saved;
  if (!draft.id || !saved) return null;
  return remoteHeaderPolicyIssue(
    {
      name: saved.name,
      valueFromRequestHeader:
        saved.source === "request" ? saved.valueFromRequestHeader : undefined,
      isRequired: saved.isRequired,
    },
    "stored",
  );
}

/**
 * The header policy's verdict on a draft as it would be written. An unsaved
 * source counts only when the row reads from a request header.
 */
function draftPolicyIssue(draft: HeaderDraft): RemoteHeaderPolicyIssue | null {
  return remoteHeaderPolicyIssue(
    {
      name: draft.name,
      valueFromRequestHeader:
        draft.source === "request" ? draft.valueFromRequestHeader : undefined,
      isRequired: draft.isRequired,
    },
    "write",
  );
}

// A saved secret shows its redacted placeholder (`***`) in the value field. As
// long as the user leaves that placeholder untouched, we keep the existing
// secret rather than overwriting it with the literal redaction string.
function isUntouchedSecret(draft: HeaderDraft): boolean {
  return draft.hadSecret && draft.staticValue === REDACTED_SECRET;
}

export function draftsEqual(a: HeaderDraft[], b: HeaderDraft[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index += 1) {
    const draft = a[index];
    const other = b[index];
    if (!draft || !other) return false;
    if (
      draft.id !== other.id ||
      draft.name !== other.name ||
      draft.source !== other.source ||
      draft.staticValue !== other.staticValue ||
      draft.valueFromRequestHeader !== other.valueFromRequestHeader ||
      draft.isRequired !== other.isRequired ||
      draft.isSecret !== other.isSecret
    ) {
      return false;
    }
  }
  return true;
}

/** Which field a problem belongs to, so the row can point at the right one. */
export type HeaderDraftError = {
  readonly field: "name" | "value";
  readonly message: string;
};

/**
 * The first problem on each row, keyed by the draft's key.
 *
 * Keyed per row rather than returned as one message because the form marks the
 * offending field, and a single string cannot say which field that is. The
 * scan continues past a bad row so every one of them gets marked, not just the
 * first.
 */
export function headerDraftErrors(
  drafts: HeaderDraft[],
  identityMode?: IdentityMode,
  managedAuthorizationHeaderId?: string,
): ReadonlyMap<string, HeaderDraftError> {
  const errors = new Map<string, HeaderDraftError>();
  const names = new Set<string>();

  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name) {
      errors.set(draft.key, {
        field: "name",
        message: "Every header needs a name.",
      });
      continue;
    }

    const normalized = name.toLowerCase();
    if (names.has(normalized)) {
      errors.set(draft.key, {
        field: "name",
        message: `Duplicate header name "${name}".`,
      });
      continue;
    }
    names.add(normalized);

    // A Service Account writes its own static Authorization row, so a saved
    // row claiming that name has to go first. Otherwise an untouched saved
    // row is left to the policy check below rather than blocking every edit.
    if (
      identityMode &&
      !(identityMode !== "agent" && isUnchangedSavedDraft(draft))
    ) {
      const authorizationError = authorizationHeaderGuard(
        identityMode,
        name,
        !!draft.id && draft.id === managedAuthorizationHeaderId,
      );
      if (authorizationError) {
        errors.set(draft.key, { field: "name", message: authorizationError });
        continue;
      }
    }

    // A saved row is checked when it is edited, not merely for existing:
    // the server leaves untouched rows alone, so one the policy now refuses
    // must not hold every other row hostage. Its row explains the refusal.
    if (isUnchangedSavedDraft(draft)) continue;

    const policyIssue = draftPolicyIssue(draft);
    if (policyIssue) {
      errors.set(draft.key, {
        field: policyIssue.field === "source" ? "value" : "name",
        message: draft.id
          ? `Change the source or name of "${name}", or remove this header.`
          : remoteHeaderPolicyReasonMessage(policyIssue.reason, draft),
      });
      continue;
    }

    if (draft.source === "request") {
      const source = draft.valueFromRequestHeader.trim();
      if (!source) {
        errors.set(draft.key, {
          field: "value",
          message: `Header "${name}" needs an inbound request header name.`,
        });
        continue;
      }
    }

    if (draft.source === "static" && isUntouchedSecret(draft)) {
      // Never write the literal placeholder as the credential, and never ask
      // the server to reveal the stored secret as plain text.
      if (!draft.isSecret) {
        errors.set(draft.key, {
          field: "value",
          message: `Enter a new value for "${name}" to store it as non-secret.`,
        });
      }
      continue;
    }

    if (draft.source === "static" && draft.staticValue.trim() === "") {
      errors.set(draft.key, {
        field: "value",
        message: `Header "${name}" needs a static value.`,
      });
    }
  }

  return errors;
}

/** The one message to show for the list: the topmost row's problem. */
export function validateDrafts(
  drafts: HeaderDraft[],
  identityMode?: IdentityMode,
  managedAuthorizationHeaderId?: string,
): string | null {
  const errors = headerDraftErrors(
    drafts,
    identityMode,
    managedAuthorizationHeaderId,
  );
  for (const draft of drafts) {
    const error = errors.get(draft.key);
    if (error) return error.message;
  }
  return null;
}

export function newHeaderDraft(): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name: "",
    source: "static",
    staticValue: "",
    valueFromRequestHeader: "",
    isRequired: false,
    isSecret: true,
    hadSecret: false,
  };
}

export function headerDraftFromCatalog(
  header: ExternalMCPRemoteHeader,
): HeaderDraft {
  return {
    ...newHeaderDraft(),
    fromCatalog: true,
    name: header.name,
    isRequired: header.isRequired ?? false,
    // Registries omit is_secret inconsistently; default suggested headers to
    // secret so an API key never lands in plain text by accident.
    isSecret: header.isSecret ?? true,
  };
}

export type HeaderWriteFields = {
  name: string;
  isRequired: boolean;
  isSecret?: boolean;
  value?: string;
  valueFromRequestHeader?: string;
};

export function headerDraftToWriteFields(
  draft: HeaderDraft,
): HeaderWriteFields {
  const base = {
    name: draft.name.trim(),
    isRequired: draft.isRequired,
  };

  if (draft.source === "request") {
    return {
      ...base,
      isSecret: false,
      valueFromRequestHeader: draft.valueFromRequestHeader.trim(),
    };
  }

  if (isUntouchedSecret(draft)) {
    // The server keeps a stored value only for a row that stays secret, and
    // never reveals one as plain text. Omitting `value` keeps it; un-ticking
    // Secret needs a fresh value, which headerDraftErrors already demands —
    // refuse here too so the placeholder can never become the credential.
    if (!draft.isSecret) {
      throw new Error(`Header "${base.name}" needs a new value.`);
    }
    return { ...base, isSecret: true };
  }

  return {
    ...base,
    isSecret: draft.isSecret,
    value: draft.staticValue,
  };
}

// A single remote_mcps row can back several mcp_servers rows. Its headers are
// stored on the remote, so editing them from any one MCP server silently
// rewrites the values every sibling server sends. HeadersSectionContext tells
// the component which surface it's rendered from so it can guard against that:
//  - "mcp-server": rendered on an MCP server's Settings tab. When the backing
//    remote is shared by more than one server, editing is locked and the user
//    is pointed at the Remote MCP source, the single canonical edit surface.
//  - "remote-mcp": rendered on the Remote MCP source page. Always editable,
