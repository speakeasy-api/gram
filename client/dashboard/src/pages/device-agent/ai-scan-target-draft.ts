import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertAiScanTargetRequestBody } from "@gram/client/models/components/upsertaiscantargetrequestbody.js";

// Form state and rules for the scan target editor; the rules mirror the
// server's aitargets.Validate.

export type TargetCategory = "harness" | "assistant" | "local_model";

export const TARGET_CATEGORIES: ReadonlyArray<{
  value: TargetCategory;
  // label names one tool of this kind: a row, a select option. plural names
  // the kind as a group heading. The two read wrong in each other's place.
  label: string;
  plural: string;
  description: string;
}> = [
  {
    value: "harness",
    label: "Harness",
    plural: "Harnesses",
    description:
      "An agentic coding tool or AI IDE, such as Claude Code or Cursor.",
  },
  {
    value: "assistant",
    label: "Assistant",
    plural: "Assistants",
    description:
      "A general-purpose AI assistant or agent, such as OpenClaw or Hermes.",
  },
  {
    value: "local_model",
    label: "Open model",
    plural: "Open models",
    description: "An open model run locally, such as Ollama or LM Studio.",
  },
];

// The form edits the name, category, binaries, config dirs, process names,
// and the gateway-client matchers. The other fields ride along from an
// existing target so an edit never wipes them: the id is derived from the
// name on create and fixed afterwards, and bundle ids and the plist key are
// only set outside the form.
export type Draft = {
  id: string;
  displayName: string;
  category: TargetCategory;
  bundleIds: string[];
  binaries: string[];
  configDirs: string[];
  processNames: string[];
  versionPlistKey: string;
  cimdVendorKeys: string[];
  oauthClientIds: string[];
  clientInfoNames: string[];
};

export type DraftErrors = Partial<Record<keyof Draft, string>>;

const MAX_SIGNATURE_ENTRIES = 16;
const MAX_GATEWAY_CLIENT_ENTRIES = 16;
const MAX_CLIENT_ID_LENGTH = 512;
const ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;
const BUNDLE_ID_PATTERN = /^[A-Za-z0-9._-]{1,128}$/;
const BINARY_PATTERN = /^[A-Za-z0-9._-]{1,64}$/;
const PROCESS_NAME_PATTERN = /^[A-Za-z0-9 ._-]{1,64}$/;
const PLIST_KEY_PATTERN = /^[A-Za-z0-9]{1,64}$/;

export function emptyDraft(): Draft {
  return {
    id: "",
    displayName: "",
    category: "harness",
    bundleIds: [],
    binaries: [],
    configDirs: [],
    processNames: [],
    versionPlistKey: "",
    cimdVendorKeys: [],
    oauthClientIds: [],
    clientInfoNames: [],
  };
}

// draftFromTarget seeds the editor from an existing target. The category is
// carried through as-is: the API types it as a closed enum over the same
// values as TargetCategory, so a server-side addition breaks this assignment
// at compile time rather than silently rewriting the target's category on the
// next upsert.
export function draftFromTarget(target: AiScanTarget): Draft {
  return {
    id: target.id,
    displayName: target.displayName,
    category: target.category,
    bundleIds: [...target.signatures.bundleIds],
    binaries: [...target.signatures.binaries],
    configDirs: [...target.signatures.configDirs],
    processNames: [...target.signatures.processNames],
    versionPlistKey: target.versionPlistKey ?? "",
    cimdVendorKeys: [...target.gatewayClient.cimdVendorKeys],
    oauthClientIds: [...target.gatewayClient.oauthClientIds],
    clientInfoNames: [...target.gatewayClient.clientInfoNames],
  };
}

// slugFromName derives the id agents report from the display name: accents
// stripped, lowercased, runs of anything but letters and digits collapsed to
// one hyphen, and cut to the 64 characters the server allows.
export function slugFromName(name: string): string {
  return name
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64)
    .replace(/-+$/, "");
}

// codePoints counts characters the way the server does, so a name or path
// full of multibyte characters is judged by the same limit on both sides.
function codePoints(value: string): number {
  return Array.from(value).length;
}

// hasControlCharacter covers what Go's unicode.IsControl does on the server:
// the C0 range, DEL and the C1 range. Scanned rather than matched in a regex,
// which the lint rules forbid for control characters.
function hasControlCharacter(value: string): boolean {
  return Array.from(value).some((char) => {
    const code = char.codePointAt(0) ?? 0;
    return code < 0x20 || (code >= 0x7f && code <= 0x9f);
  });
}

// normalizeConfigDir just trims what was typed. The path is taken as
// home-relative unless it starts with /, and the device agent resolves it.
export function normalizeConfigDir(dir: string): string {
  return dir.trim();
}

function listProblem(
  entries: string[],
  noun: string,
  accept: (entry: string) => string | undefined,
): string | undefined {
  if (entries.length > MAX_SIGNATURE_ENTRIES) {
    return `At most ${MAX_SIGNATURE_ENTRIES} ${noun} are allowed`;
  }
  for (const entry of entries) {
    const problem = accept(entry);
    if (problem) return problem;
  }
  return undefined;
}

function gatewayListProblem(
  entries: string[],
  noun: string,
  accept: (entry: string) => string | undefined,
): string | undefined {
  if (entries.length > MAX_GATEWAY_CLIENT_ENTRIES) {
    return `At most ${MAX_GATEWAY_CLIENT_ENTRIES} ${noun} are allowed`;
  }
  for (const entry of entries) {
    const problem = accept(entry);
    if (problem) return problem;
  }
  return undefined;
}

// callsGateway mirrors the server's aitargets.CallsGateway: which categories
// ever reach Gram's MCP gateway, and so which ones it is meaningful to name a
// caller for. An open model run locally never connects.
export function callsGateway(category: TargetCategory): boolean {
  return category === "harness" || category === "assistant";
}

// withCategory switches the kind of tool a draft describes. The gateway
// matchers go with a switch to a kind that never calls the gateway: the form
// hides those fields for an open model, so anything left in them could be
// neither seen nor cleared, and the server refuses a target that carries them.
export function withCategory(draft: Draft, category: TargetCategory): Draft {
  if (callsGateway(category)) return { ...draft, category };
  return {
    ...draft,
    category,
    cimdVendorKeys: [],
    oauthClientIds: [],
    clientInfoNames: [],
  };
}

export function validateDraft(draft: Draft): DraftErrors {
  const errors: DraftErrors = {};

  if (draft.id.trim() === "") {
    errors.id = "The name needs at least one letter or digit to make an id";
  } else if (!ID_PATTERN.test(draft.id.trim())) {
    errors.id =
      "Use lowercase letters, digits and hyphens, starting with a letter or digit (max 64)";
  }
  const name = draft.displayName.trim();
  if (name === "" || codePoints(name) > 128) {
    errors.displayName = "Enter a display name of at most 128 characters";
  }

  const { bundleIds, binaries, configDirs, processNames } = draft;

  errors.bundleIds = listProblem(bundleIds, "bundle ids", (id) =>
    BUNDLE_ID_PATTERN.test(id)
      ? undefined
      : `"${id}" must be a bundle identifier (letters, digits, dots, hyphens, underscores)`,
  );
  errors.binaries = listProblem(binaries, "binaries", (bin) =>
    BINARY_PATTERN.test(bin) && bin !== "." && bin !== ".."
      ? undefined
      : `"${bin}" must be a bare command name, not a path`,
  );
  errors.configDirs = listProblem(configDirs, "config dirs", () => undefined);
  errors.processNames = listProblem(processNames, "process names", (proc) =>
    PROCESS_NAME_PATTERN.test(proc)
      ? undefined
      : `"${proc}" must be a plain process name (letters, digits, spaces, dots, hyphens, underscores)`,
  );

  if (
    !errors.bundleIds &&
    !errors.binaries &&
    !errors.configDirs &&
    bundleIds.length + binaries.length + configDirs.length === 0
  ) {
    errors.binaries =
      "Add at least one install signature: a binary or a config dir";
  }

  const plistKey = draft.versionPlistKey.trim();
  if (plistKey !== "" && !PLIST_KEY_PATTERN.test(plistKey)) {
    errors.versionPlistKey =
      "Use an Info.plist key made of letters and digits only";
  }

  // A harness and an assistant both reach Gram's MCP gateway, so both may
  // carry matchers. An open model never does, and its draft never holds any:
  // withCategory drops them on the switch and draftToUpsertBody sends none.
  // There is nothing to check for it, and nowhere to show a finding, since
  // the form hides the matcher fields for that category.
  if (callsGateway(draft.category)) {
    errors.oauthClientIds = gatewayListProblem(
      draft.oauthClientIds,
      "documents",
      clientIdProblem,
    );
    errors.clientInfoNames = gatewayListProblem(
      draft.clientInfoNames,
      "client names",
      (name) =>
        codePoints(name) > 128
          ? `"${name}" is longer than 128 characters`
          : undefined,
    );
  }

  for (const key of Object.keys(errors) as Array<keyof Draft>) {
    if (errors[key] === undefined) delete errors[key];
  }
  return errors;
}

// clientIdProblem holds a client id to the shape the server accepts (its
// validateClientIDURLShape), so an entry the save would refuse is turned away
// when it is typed. Blocking is CIMD-only, and a CIMD client_id is the https
// URL its document is served from: a bare origin, a fragment, a userinfo
// component or a dot segment is a shape no client_id can take.
//
// The server stays the final gate. A malformed percent-escape in the path is
// caught because decoding the segment throws, the way Go's url.Parse rejects
// it; the host is judged by the URL parser.
function clientIdProblem(id: string): string | undefined {
  if (codePoints(id) > MAX_CLIENT_ID_LENGTH) {
    return `"${id}" is longer than ${MAX_CLIENT_ID_LENGTH} characters`;
  }
  if (/\s/.test(id) || hasControlCharacter(id)) {
    return `"${id}" must not contain spaces`;
  }
  if (!id.startsWith("https://")) {
    return `"${id}" must be an https URL to a client ID metadata document`;
  }
  if (id.includes("#")) return `"${id}" must not contain a fragment`;
  let rest = id.slice("https://".length);
  const query = rest.indexOf("?");
  if (query >= 0) rest = rest.slice(0, query);
  const slash = rest.indexOf("/");
  if (slash < 0) {
    return `"${id}" must include a path; a bare origin is not a client id`;
  }
  const host = rest.slice(0, slash);
  if (host === "") return `"${id}" must include a host`;
  if (host.includes("@")) {
    return `"${id}" must not contain a userinfo component`;
  }
  if (!URL.canParse(id)) return `"${id}" must be a parseable https URL`;
  // Dot segments are judged on the decoded path, as the server does: Go's
  // url.Parse decodes the whole path before it is split, so an encoded slash
  // separates segments too and "a%2F.." hides nothing. The path is read from
  // the string rather than from URL.pathname, which resolves dot segments
  // away.
  let decodedPath: string;
  try {
    decodedPath = decodeURIComponent(rest.slice(slash));
  } catch {
    return `"${id}" must be a parseable https URL`;
  }
  for (const segment of decodedPath.split("/")) {
    if (segment === "." || segment === "..") {
      return `"${id}" must not contain "." or ".." path segments`;
    }
  }
  return undefined;
}

/**
 * clientIdFromCimdInput reads what someone put in the CIMD documents field and
 * returns the client_id to store.
 *
 * Two shapes are accepted because both are what people have to hand: the URL
 * the document is served from, or the document itself. They collapse to one
 * value — draft-ietf-oauth-client-id-metadata-document requires a document's
 * `client_id` member to equal the URL it was fetched from — so pasting the
 * JSON is a convenience, not a second kind of matcher.
 */
export function clientIdFromCimdInput(
  input: string,
): { clientId: string } | { error: string } {
  const trimmed = input.trim();
  if (trimmed === "") return { error: "Enter a document URL, or paste one" };

  if (!trimmed.startsWith("{")) {
    if (!trimmed.startsWith("https://")) {
      return {
        error:
          "Enter an https URL to a client ID metadata document, or paste the document itself",
      };
    }
    const problem = clientIdProblem(trimmed);
    return problem === undefined ? { clientId: trimmed } : { error: problem };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { error: "That is not valid JSON" };
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { error: "A client ID metadata document must be a JSON object" };
  }
  const clientId = (parsed as { client_id?: unknown }).client_id;
  if (typeof clientId !== "string" || !clientId.startsWith("https://")) {
    return {
      error:
        "The document has no client_id member, or it is not an https URL. A document's client_id must equal the URL it is served from.",
    };
  }
  const problem = clientIdProblem(clientId);
  return problem === undefined ? { clientId } : { error: problem };
}

export function draftToUpsertBody(draft: Draft): UpsertAiScanTargetRequestBody {
  const plistKey = draft.versionPlistKey.trim();
  return {
    id: draft.id.trim(),
    displayName: draft.displayName.trim(),
    category: draft.category,
    signatures: {
      bundleIds: draft.bundleIds,
      binaries: draft.binaries,
      configDirs: draft.configDirs,
      processNames: draft.processNames,
    },
    gatewayClient: callsGateway(draft.category)
      ? {
          cimdVendorKeys: draft.cimdVendorKeys,
          oauthClientIds: draft.oauthClientIds,
          clientInfoNames: draft.clientInfoNames,
        }
      : { cimdVendorKeys: [], oauthClientIds: [], clientInfoNames: [] },
    versionPlistKey: plistKey === "" ? undefined : plistKey,
  };
}

export function categoryLabel(category: string): string {
  return (
    TARGET_CATEGORIES.find((option) => option.value === category)?.label ??
    category
  );
}

export function signatureSummary(target: AiScanTarget): string {
  const parts: string[] = [];
  const { bundleIds, binaries, configDirs, processNames } = target.signatures;
  const count = (n: number, singular: string, plural: string): void => {
    if (n > 0) parts.push(`${n} ${n === 1 ? singular : plural}`);
  };
  count(bundleIds.length, "bundle id", "bundle ids");
  count(binaries.length, "binary", "binaries");
  count(configDirs.length, "config dir", "config dirs");
  count(processNames.length, "process name", "process names");
  return parts.length > 0 ? parts.join(" · ") : "No signatures";
}
