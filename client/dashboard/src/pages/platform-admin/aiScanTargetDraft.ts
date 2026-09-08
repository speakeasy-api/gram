import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertRequestBody2 } from "@gram/client/models/components/upsertrequestbody2.js";

// Form state and rules for the scan target editor; the rules mirror the
// server's aitargets.Validate.

export type TargetCategory = "harness" | "local_model";

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
    value: "local_model",
    label: "Local model",
    description: "A local model runtime, such as Ollama or LM Studio.",
  },
];

export type Draft = {
  id: string;
  displayName: string;
  category: TargetCategory;
  bundleIds: string;
  binaries: string;
  configDirs: string;
  processNames: string;
  versionPlistKey: string;
  enabled: boolean;
  reason: string;
};

export type DraftErrors = Partial<Record<keyof Draft, string>>;

const MAX_SIGNATURE_ENTRIES = 16;
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
    bundleIds: "",
    binaries: "",
    configDirs: "",
    processNames: "",
    versionPlistKey: "",
    enabled: true,
    reason: "",
  };
}

export function draftFromTarget(target: AiScanTarget): Draft {
  return {
    id: target.id,
    displayName: target.displayName,
    category: target.category === "local_model" ? "local_model" : "harness",
    bundleIds: target.signatures.bundleIds.join("\n"),
    binaries: target.signatures.binaries.join("\n"),
    configDirs: target.signatures.configDirs.join("\n"),
    processNames: target.signatures.processNames.join("\n"),
    versionPlistKey: target.versionPlistKey ?? "",
    enabled: target.enabled,
    reason: "",
  };
}

// parseSignatureLines splits a field on newlines, dropping blanks and
// duplicates. Newlines only: the server allows commas inside an entry.
export function parseSignatureLines(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split("\n")) {
    const value = raw.trim();
    if (value === "" || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

// codePoints counts characters the way the server does, so a name or path
// full of multibyte characters is judged by the same limit on both sides.
function codePoints(value: string): number {
  return [...value].length;
}

function configDirProblem(dir: string): string | undefined {
  if (codePoints(dir) > 256) return `"${dir}" is longer than 256 characters`;
  if (!dir.startsWith("~/")) return `"${dir}" must start with ~/`;
  if (/[\\\0]/.test(dir)) return `"${dir}" must not contain backslashes`;
  for (const segment of dir.slice(2).split("/")) {
    if (segment === "" || segment === "." || segment === "..") {
      return `"${dir}" must not contain empty, "." or ".." segments`;
    }
  }
  return undefined;
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

export function validateDraft(draft: Draft): DraftErrors {
  const errors: DraftErrors = {};

  if (!ID_PATTERN.test(draft.id.trim())) {
    errors.id =
      "Use lowercase letters, digits and hyphens, starting with a letter or digit (max 64)";
  }
  const name = draft.displayName.trim();
  if (name === "" || codePoints(name) > 128) {
    errors.displayName = "Enter a display name of at most 128 characters";
  }

  const bundleIds = parseSignatureLines(draft.bundleIds);
  const binaries = parseSignatureLines(draft.binaries);
  const configDirs = parseSignatureLines(draft.configDirs);
  const processNames = parseSignatureLines(draft.processNames);

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
  errors.configDirs = listProblem(configDirs, "config dirs", configDirProblem);
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
    errors.bundleIds =
      "Add at least one install signature: a bundle id, a binary, or a config dir";
  }

  const plistKey = draft.versionPlistKey.trim();
  if (plistKey !== "" && !PLIST_KEY_PATTERN.test(plistKey)) {
    errors.versionPlistKey =
      "Use an Info.plist key made of letters and digits only";
  }
  if (codePoints(draft.reason) > 1000) {
    errors.reason = "Keep the reason under 1000 characters";
  }

  for (const key of Object.keys(errors) as Array<keyof Draft>) {
    if (errors[key] === undefined) delete errors[key];
  }
  return errors;
}

export function draftToUpsertBody(draft: Draft): UpsertRequestBody2 {
  const plistKey = draft.versionPlistKey.trim();
  const reason = draft.reason.trim();
  return {
    id: draft.id.trim(),
    displayName: draft.displayName.trim(),
    category: draft.category,
    signatures: {
      bundleIds: parseSignatureLines(draft.bundleIds),
      binaries: parseSignatureLines(draft.binaries),
      configDirs: parseSignatureLines(draft.configDirs),
      processNames: parseSignatureLines(draft.processNames),
    },
    versionPlistKey: plistKey === "" ? undefined : plistKey,
    enabled: draft.enabled,
    reason: reason === "" ? undefined : reason,
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

const ACTION_LABELS: Record<string, string> = {
  seed: "Seeded",
  upsert: "Saved",
  enable: "Enabled",
  disable: "Disabled",
  delete: "Deleted",
};

export function revisionActionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action;
}
