import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertAiScanTargetRequestBody } from "@gram/client/models/components/upsertaiscantargetrequestbody.js";

// Form state and rules for the scan target editor; the rules mirror the
// server's aitargets.Validate.

export type TargetCategory = "harness" | "assistant" | "local_model";

export const TARGET_CATEGORIES: ReadonlyArray<{
  value: TargetCategory;
  label: string;
  description: string;
}> = [
  {
    value: "harness",
    label: "Harness",
    description:
      "An agentic coding tool or AI IDE, such as Claude Code or Cursor.",
  },
  {
    value: "assistant",
    label: "Assistants",
    description:
      "A general-purpose AI assistant or agent, such as OpenClaw or Hermes.",
  },
  {
    value: "local_model",
    label: "Open models",
    description: "An open model run locally, such as Ollama or LM Studio.",
  },
];

// The form edits the name, category, binaries, config dirs, process names,
// and the gateway-client matchers. The other fields ride along from an
// existing target so an edit never wipes them: the id is derived from the
// name on create and fixed afterwards, and bundle ids, the plist key, and the
// served flag are only set outside the form.
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
  enabled: boolean;
};

export type DraftErrors = Partial<Record<keyof Draft, string>>;

const MAX_SIGNATURE_ENTRIES = 16;
const MAX_GATEWAY_CLIENT_ENTRIES = 16;
const ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;
const BUNDLE_ID_PATTERN = /^[A-Za-z0-9._-]{1,128}$/;
const BINARY_PATTERN = /^[A-Za-z0-9._-]{1,64}$/;
const PROCESS_NAME_PATTERN = /^[A-Za-z0-9 ._-]{1,64}$/;
const PLIST_KEY_PATTERN = /^[A-Za-z0-9]{1,64}$/;

function isTargetCategory(value: string): value is TargetCategory {
  return TARGET_CATEGORIES.some((option) => option.value === value);
}

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
    enabled: true,
  };
}

export function draftFromTarget(target: AiScanTarget): Draft {
  return {
    id: target.id,
    displayName: target.displayName,
    category: isTargetCategory(target.category) ? target.category : "harness",
    bundleIds: [...target.signatures.bundleIds],
    binaries: [...target.signatures.binaries],
    configDirs: [...target.signatures.configDirs],
    processNames: [...target.signatures.processNames],
    versionPlistKey: target.versionPlistKey ?? "",
    cimdVendorKeys: [...target.gatewayClient.cimdVendorKeys],
    oauthClientIds: [...target.gatewayClient.oauthClientIds],
    clientInfoNames: [...target.gatewayClient.clientInfoNames],
    enabled: target.enabled,
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
  // carry matchers. The form hides these fields for an open model; this
  // catches a category switch that would otherwise leave stale ones behind.
  if (!callsGateway(draft.category)) {
    if (
      draft.oauthClientIds.length > 0 ||
      draft.clientInfoNames.length > 0 ||
      draft.cimdVendorKeys.length > 0
    ) {
      errors.oauthClientIds =
        "Only a harness or assistant can carry gateway client matchers";
    }
  } else {
    errors.oauthClientIds = gatewayListProblem(
      draft.oauthClientIds,
      "documents",
      (id) => {
        if (codePoints(id) > 512)
          return `"${id}" is longer than 512 characters`;
        if (/[\s\u0000-\u001f]/.test(id))
          return `"${id}" must not contain spaces`;
        // Blocking is CIMD-only, and a CIMD client_id is the https URL its
        // document is served from.
        if (!id.startsWith("https://")) {
          return `"${id}" must be an https URL to a client ID metadata document`;
        }
        return undefined;
      },
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
    return trimmed.startsWith("https://")
      ? { clientId: trimmed }
      : {
          error:
            "Enter an https URL to a client ID metadata document, or paste the document itself",
        };
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
  return { clientId };
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
    enabled: draft.enabled,
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
