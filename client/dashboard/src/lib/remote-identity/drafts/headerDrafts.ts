import type { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { authorizationHeaderGuard } from "../model/headers";
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
};

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

  return {
    key: header.id,
    id: header.id,
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
    hadSecret: header.isSecret,
  };
}

// Inbound headers a pass-through row may not read from; mirrors the proxy.
// Authorization is deliberately absent: forwarding the caller's own upstream
// credential is what pass-through identity is for.
const DENIED_PASS_THROUGH_SOURCES = new Set([
  "cookie",
  "set-cookie",
  "proxy-authorization",
]);

// A saved secret shows its redacted placeholder (`***`) in the value field. As
// long as the user leaves that placeholder untouched, we keep the existing
// secret rather than overwriting it with the literal redaction string.
function isUnchangedSecret(draft: HeaderDraft): boolean {
  // Deliberately not conditioned on the current isSecret toggle: what makes
  // the value unchanged is that the placeholder was never typed over. Reading
  // the toggle here meant that un-ticking Secret on an untouched row stored
  // the literal redaction string as the upstream credential.
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

    if (identityMode) {
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

    if (draft.source === "request") {
      const source = draft.valueFromRequestHeader.trim();
      if (!source) {
        errors.set(draft.key, {
          field: "value",
          message: `Header "${name}" needs an inbound request header name.`,
        });
        continue;
      }
      // Mirrors the proxy, which is the control that actually holds: these
      // carry the dashboard's own session rather than anything meant for the
      // upstream. Checked here so the refusal arrives while editing instead of
      // as a failed request later.
      if (DENIED_PASS_THROUGH_SOURCES.has(source.toLowerCase())) {
        errors.set(draft.key, {
          field: "value",
          message: `"${source}" cannot be forwarded upstream.`,
        });
        continue;
      }
    }

    if (
      draft.source === "static" &&
      !isUnchangedSecret(draft) &&
      draft.staticValue.trim() === ""
    ) {
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

  if (isUnchangedSecret(draft)) {
    // Omitting `value` is what tells the server to keep the stored secret, so
    // the toggle can still travel on its own.
    return {
      ...base,
      isSecret: draft.isSecret,
    };
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
