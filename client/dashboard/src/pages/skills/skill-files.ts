import { unzipSync } from "fflate";
import { decodeManifestFile, MAX_SKILL_MANIFEST_BYTES } from "./skill-manifest";

// These limits mirror the server's skilltree package so an upload is rejected
// before it leaves the browser, with the same rules the API would apply.
export const MAX_SKILL_FILES = 100;
export const MAX_SKILL_FILE_BYTES = 5 * 1024 * 1024;
const MAX_SKILL_FILES_TOTAL_BYTES = 10 * 1024 * 1024;
const MAX_SKILL_FILE_PATH_BYTES = 512;
export const SKILL_MANIFEST_PATH = "SKILL.md";

/** A file read from an uploaded folder or zip, before the skill is parsed. */
export type SkillUploadEntry = {
  path: string;
  /** Bytes, or null when the file was too large to read. */
  content: Uint8Array | null;
  sizeBytes: number;
};

/** A supporting file in the dialog: either new bytes or a file kept from the base version. */
export type DraftSkillFile =
  | { kind: "new"; path: string; content: Uint8Array; sizeBytes: number }
  | { kind: "existing"; path: string; sizeBytes: number; contentType: string };

export type ParsedSkillUpload = {
  manifest: string;
  files: DraftSkillFile[];
  /** The single top-level folder the upload was wrapped in, if any. */
  rootDir: string | null;
};

export class SkillUploadError extends Error {}

/** Metadata the dashboard needs to compare or list files. */
export type SkillFileMeta = {
  path: string;
  sha256: string;
  sizeBytes: number;
  contentType: string;
};

const encoder = new TextEncoder();

function byteLength(value: string): number {
  return encoder.encode(value).byteLength;
}

// Files operating systems add to folders and archives. They are never part of
// a skill, so they are dropped rather than reported.
function isSystemJunk(path: string): boolean {
  const segments = path.split("/");
  const base = segments[segments.length - 1] ?? "";
  return (
    segments[0] === "__MACOSX" ||
    base === ".DS_Store" ||
    base === "Thumbs.db" ||
    base === "desktop.ini"
  );
}

function isManifestPath(path: string): boolean {
  return path.toLowerCase() === SKILL_MANIFEST_PATH.toLowerCase();
}

/**
 * Turns the raw entries of a folder or zip into a SKILL.md plus supporting
 * files. A single wrapping folder is stripped, so `my-skill/SKILL.md` and
 * `SKILL.md` both work. Throws SkillUploadError when there is no manifest.
 */
export function parseSkillUpload(
  rawEntries: SkillUploadEntry[],
): ParsedSkillUpload {
  const entries = rawEntries
    .map((entry) => ({ ...entry, path: entry.path.replace(/\\/g, "/") }))
    .filter((entry) => entry.path !== "" && !entry.path.endsWith("/"))
    .filter((entry) => !isSystemJunk(entry.path));

  if (entries.length === 0) {
    throw new SkillUploadError("The upload is empty.");
  }

  let rootDir: string | null = null;
  const firstSegments = new Set(entries.map((e) => e.path.split("/")[0]));
  const allNested = entries.every((e) => e.path.includes("/"));
  if (firstSegments.size === 1 && allNested) {
    rootDir = [...firstSegments][0] ?? null;
  }
  const relative = entries.map((entry) => ({
    ...entry,
    path: rootDir ? entry.path.slice(rootDir.length + 1) : entry.path,
  }));

  const manifestEntry = relative.find((entry) => isManifestPath(entry.path));
  if (!manifestEntry) {
    throw new SkillUploadError(
      "No SKILL.md found at the top of the upload. A skill folder must contain SKILL.md.",
    );
  }
  if (
    manifestEntry.content === null ||
    manifestEntry.sizeBytes > MAX_SKILL_MANIFEST_BYTES
  ) {
    throw new SkillUploadError(
      `SKILL.md must be 65,536 bytes or fewer (currently ${manifestEntry.sizeBytes.toLocaleString()} bytes).`,
    );
  }
  let manifest: string;
  try {
    manifest = decodeManifestFile(manifestEntry.content.slice().buffer);
  } catch {
    throw new SkillUploadError("SKILL.md is not valid UTF-8.");
  }

  const files: DraftSkillFile[] = relative
    .filter((entry) => entry !== manifestEntry)
    .map((entry) => ({
      kind: "new" as const,
      path: entry.path,
      // Oversized files keep their size so validation can name them; their
      // bytes are never read.
      content: entry.content ?? new Uint8Array(),
      sizeBytes: entry.sizeBytes,
    }))
    .sort((a, b) => compareStrings(a.path, b.path));

  return { manifest, files, rootDir };
}

function compareStrings(a: string, b: string): number {
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}

/** Mirrors skilltree.ValidatePath. Returns a message, or null when valid. */
export function validateSkillFilePath(path: string): string | null {
  if (path === "") return "A file path is empty.";
  if (byteLength(path) > MAX_SKILL_FILE_PATH_BYTES)
    return `${path} has a path longer than ${MAX_SKILL_FILE_PATH_BYTES} bytes.`;
  if (path.startsWith("/"))
    return `${path} must be relative to the skill folder.`;
  if (path.includes("\\")) return `${path} must use forward slashes.`;
  // Control characters are never valid in a path.
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f-\u009f]/.test(path))
    return `${path} contains a control character.`;
  const segments = path.split("/");
  if (segments.some((s) => s === ".."))
    return `${path} points outside the skill folder.`;
  if (segments.some((s) => s === "" || s === "."))
    return `${path} has an empty or "." folder in its path.`;
  if (isManifestPath(path))
    return `${path} is the skill manifest, not a supporting file.`;
  return null;
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`;
}

/**
 * Mirrors skilltree.Validate: file count, path rules, case-only duplicates,
 * per-file size, and total size. Returns every problem found.
 */
export function validateSkillFiles(
  files: ReadonlyArray<{ path: string; sizeBytes: number }>,
): string[] {
  const errors: string[] = [];
  if (files.length > MAX_SKILL_FILES) {
    errors.push(
      `A skill can have at most ${MAX_SKILL_FILES} supporting files (this one has ${files.length}).`,
    );
  }
  const seen = new Map<string, string>();
  let total = 0;
  for (const file of files) {
    const pathError = validateSkillFilePath(file.path);
    if (pathError) errors.push(pathError);
    const folded = file.path.toLowerCase();
    const previous = seen.get(folded);
    if (previous !== undefined) {
      errors.push(
        `${previous} and ${file.path} differ only by letter case. Rename one of them.`,
      );
    } else {
      seen.set(folded, file.path);
    }
    if (file.sizeBytes > MAX_SKILL_FILE_BYTES) {
      errors.push(
        `${file.path} is ${formatFileSize(file.sizeBytes)}. Each file must be 5 MB or smaller.`,
      );
    }
    total += file.sizeBytes;
  }
  if (total > MAX_SKILL_FILES_TOTAL_BYTES) {
    errors.push(
      `Supporting files add up to ${formatFileSize(total)}. The total must be 10 MB or smaller.`,
    );
  }
  return errors;
}

/** Reads the `name` field from SKILL.md frontmatter, if present. */
export function manifestNameFromContent(content: string): string | null {
  const normalized = content.replace(/^﻿/, "").replace(/\r\n?/g, "\n");
  const lines = normalized.split("\n");
  if (lines[0]?.trimEnd() !== "---") return null;
  for (let i = 1; i < lines.length; i += 1) {
    const line = lines[i] ?? "";
    if (line.trimEnd() === "---") return null;
    const match = /^name:\s*(.*?)\s*$/.exec(line);
    if (match) {
      const value = match[1] ?? "";
      const unquoted = value.replace(/^(["'])(.*)\1$/, "$2");
      return unquoted === "" ? null : unquoted;
    }
  }
  return null;
}

/**
 * The Agent Skills spec requires a skill's name to match its folder name.
 * Returns a warning when an uploaded folder's name disagrees with SKILL.md.
 */
export function rootNameWarning(
  manifest: string,
  rootDir: string | null,
): string | null {
  if (!rootDir) return null;
  const name = manifestNameFromContent(manifest);
  if (!name || name === rootDir) return null;
  return `The skill is named "${name}" but its folder is "${rootDir}". The Agent Skills spec expects them to match.`;
}

/** Reads an uploaded .zip into entries without inflating oversized files. */
export function readSkillZip(bytes: Uint8Array): SkillUploadEntry[] {
  const sizes = new Map<string, number>();
  let unzipped: Record<string, Uint8Array>;
  try {
    unzipped = unzipSync(bytes, {
      filter: (file) => {
        sizes.set(file.name, file.originalSize);
        // Skip inflating anything that would be rejected anyway, which also
        // keeps a zip bomb from exhausting memory.
        return file.originalSize <= MAX_SKILL_FILE_BYTES;
      },
    });
  } catch {
    throw new SkillUploadError(
      "The selected file is not a valid .zip archive.",
    );
  }
  return [...sizes.entries()].map(([path, sizeBytes]) => ({
    path,
    content: unzipped[path] ?? null,
    sizeBytes,
  }));
}

/** Reads the files picked through a folder input. */
export async function readSkillFolder(
  files: ArrayLike<File>,
): Promise<SkillUploadEntry[]> {
  return Promise.all(
    Array.from(files).map(async (file) => ({
      path: file.webkitRelativePath || file.name,
      content:
        file.size <= MAX_SKILL_FILE_BYTES
          ? new Uint8Array(await file.arrayBuffer())
          : null,
      sizeBytes: file.size,
    })),
  );
}

export type SkillFileChangeStatus =
  | "added"
  | "removed"
  | "changed"
  | "unchanged";

export type SkillFileChange = {
  path: string;
  status: SkillFileChangeStatus;
  before?: SkillFileMeta;
  after?: SkillFileMeta;
};

/** Compares two versions' supporting files by path and SHA-256. */
export function diffSkillFiles(
  before: ReadonlyArray<SkillFileMeta>,
  after: ReadonlyArray<SkillFileMeta>,
): SkillFileChange[] {
  const beforeByPath = new Map(before.map((f) => [f.path, f]));
  const afterByPath = new Map(after.map((f) => [f.path, f]));
  const paths = [...new Set([...beforeByPath.keys(), ...afterByPath.keys()])];
  return paths.sort(compareStrings).map((path) => {
    const b = beforeByPath.get(path);
    const a = afterByPath.get(path);
    let status: SkillFileChangeStatus = "unchanged";
    if (!b) status = "added";
    else if (!a) status = "removed";
    else if (b.sha256 !== a.sha256) status = "changed";
    return { path, status, before: b, after: a };
  });
}

export function summarizeFileChanges(changes: SkillFileChange[]): {
  added: number;
  removed: number;
  changed: number;
} {
  return {
    added: changes.filter((c) => c.status === "added").length,
    removed: changes.filter((c) => c.status === "removed").length,
    changed: changes.filter((c) => c.status === "changed").length,
  };
}

/**
 * The spec reserves scripts/ for code agents can run. The dashboard marks those
 * files so reviewers know to read them before distributing a skill.
 */
export function isSkillScript(path: string): boolean {
  return path.startsWith("scripts/");
}

const TEXT_EXTENSIONS = new Set([
  "md",
  "markdown",
  "txt",
  "py",
  "sh",
  "bash",
  "zsh",
  "js",
  "mjs",
  "cjs",
  "ts",
  "tsx",
  "jsx",
  "json",
  "yaml",
  "yml",
  "toml",
  "ini",
  "cfg",
  "csv",
  "tsv",
  "xml",
  "html",
  "css",
  "sql",
  "rb",
  "go",
  "rs",
  "java",
  "kt",
  "swift",
  "c",
  "h",
  "cpp",
  "ps1",
  "env",
  "svg",
]);

function extension(path: string): string {
  const base = path.split("/").pop() ?? "";
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
}

/** Whether a file is likely text, from its media type or extension. */
export function isLikelyTextFile(file: {
  path: string;
  contentType: string;
}): boolean {
  const type = file.contentType.toLowerCase();
  if (type.startsWith("text/")) return true;
  if (
    /^application\/(json|yaml|x-yaml|xml|javascript|toml|x-sh|x-python)\b/.test(
      type,
    ) ||
    /\+(json|xml)\b/.test(type)
  ) {
    return true;
  }
  return TEXT_EXTENSIONS.has(extension(file.path));
}

/** A short label for a media type, without parameters such as charset. */
export function describeContentType(contentType: string): string {
  return contentType.split(";")[0]?.trim() || "unknown type";
}

export function isMarkdownFile(path: string): boolean {
  const ext = extension(path);
  return ext === "md" || ext === "markdown";
}

/** Decodes bytes as UTF-8, or returns null when they are not valid text. */
export function decodeTextContent(bytes: Uint8Array): string | null {
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    // NUL bytes mean a binary file that happens to decode.
    return text.includes("\u0000") ? null : text;
  } catch {
    return null;
  }
}

const LANGUAGES: Record<string, string> = {
  md: "markdown",
  markdown: "markdown",
  py: "python",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  jsx: "jsx",
  ts: "typescript",
  tsx: "tsx",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  xml: "xml",
  svg: "xml",
  html: "html",
  css: "css",
  sql: "sql",
  rb: "ruby",
  go: "go",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  h: "c",
  cpp: "cpp",
  ps1: "powershell",
  csv: "csv",
  ini: "ini",
};

/** The syntax highlighting language for a file path. */
export function languageForPath(path: string): string {
  return LANGUAGES[extension(path)] ?? "text";
}

export function bytesFromContent(content: Uint8Array | string): Uint8Array {
  if (typeof content !== "string") return content;
  const binary = atob(content);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

/** Saves bytes to the user's computer under the file's base name. */
export function downloadSkillFile(
  path: string,
  bytes: Uint8Array,
  contentType: string,
): void {
  const blob = new Blob([bytes.slice().buffer], {
    type: contentType || "application/octet-stream",
  });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = path.split("/").pop() || "file";
  link.click();
  URL.revokeObjectURL(url);
}
