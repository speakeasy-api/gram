import path from "node:path";
import crypto from "node:crypto";
import { TextDecoder } from "node:util";
import { readFile, readdir, stat } from "node:fs/promises";

import {
  BUILTIN_IGNORE_GLOBS,
  CAPTURE_LIMITS,
  type CaptureLimits,
  type ResolutionStatus,
} from "./constants.mts";
import { stripRegistryManagedFrontmatter } from "./frontmatter.mts";

export interface CaptureUploadRequest {
  method: "POST";
  url: string;
  headers: Record<string, string>;
  body: Buffer;
}

export interface SkillUploadMetadata {
  name: string;
  scope?: string;
  discovery_root?: string;
  source_type: "local_filesystem";
  content_sha256?: string;
  asset_format?: "zip";
  resolution_status: ResolutionStatus;
}

interface CanonicalFileRef {
  relPath: string;
  fullPath: string;
  size: number;
}

interface CanonicalFileWithContent {
  relPath: string;
  content: Buffer;
}

interface CanonicalFilesCollection {
  files: CanonicalFileRef[];
  totalBytes: number;
  estimatedTooLargeForCompression: boolean;
  errorStatus?: undefined;
}

interface CanonicalFilesCollectionError {
  errorStatus: ResolutionStatus;
}

type CanonicalFilesCollectionResult =
  | CanonicalFilesCollection
  | CanonicalFilesCollectionError;

interface CanonicalSnapshotInternal {
  errorStatus: ResolutionStatus | null;
  files: CanonicalFileWithContent[];
  fileCount: number;
  totalBytes: number;
  contentSha256: string | null;
  zipBuffer: Buffer | null;
}

export interface CanonicalArchiveSnapshot {
  errorStatus: ResolutionStatus | null;
  contentSha256: string | null;
  zipBuffer: Buffer | null;
  fileCount: number;
  totalBytes: number;
}

export interface CanonicalHashResult {
  errorStatus: ResolutionStatus | null;
  contentSha256: string | null;
  fileCount: number;
  totalBytes: number;
}

export interface DeterministicZipResult {
  errorStatus: ResolutionStatus | null;
  zipBuffer: Buffer | null;
  fileCount: number;
  totalBytes: number;
}

function normalizeString(value: any): string | null {
  if (typeof value !== "string") {
    return null;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function isRecord(value: any): value is Record<string, any> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function normalizeRelPath(relPath: string): string {
  return relPath.split(path.sep).join("/");
}

function parseGitignoreLines(content: string): string[] {
  return content
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0 && !line.startsWith("#"));
}

function matchesPattern(relPath: string, pattern: string): boolean {
  const p = pattern.startsWith("/") ? pattern.slice(1) : pattern;
  return (
    path.matchesGlob(relPath, p) ||
    path.matchesGlob(relPath, `${p}/**`) ||
    path.matchesGlob(path.posix.basename(relPath), p)
  );
}

function shouldIgnoreByPatterns(
  relPath: string,
  patterns: readonly string[],
): boolean {
  for (const pattern of patterns) {
    if (matchesPattern(relPath, pattern)) {
      return true;
    }
  }
  return false;
}

async function readGitignorePatterns(skillDir: string): Promise<string[]> {
  const ignoreFile = path.join(skillDir, ".gitignore");
  try {
    const content = await readFile(ignoreFile, "utf8");
    return parseGitignoreLines(content);
  } catch {
    return [];
  }
}

function byteStableCompare(a: string, b: string): number {
  return Buffer.compare(Buffer.from(a, "utf8"), Buffer.from(b, "utf8"));
}

const utf8Decoder = new TextDecoder("utf-8", { fatal: true });

function decodeUtf8Strict(buffer: Buffer): string | null {
  try {
    return utf8Decoder.decode(buffer);
  } catch {
    return null;
  }
}

async function collectCanonicalFiles(
  skillDir: string,
  limits: CaptureLimits = CAPTURE_LIMITS,
): Promise<CanonicalFilesCollectionResult> {
  const gitignorePatterns = await readGitignorePatterns(skillDir);

  const files: CanonicalFileRef[] = [];
  let totalBytes = 0;

  async function walk(
    dir: string,
  ): Promise<{ errorStatus: ResolutionStatus } | null> {
    const entries = await readdir(dir, { withFileTypes: true });
    entries.sort((a, b) => byteStableCompare(a.name, b.name));

    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);
      const relPath = normalizeRelPath(path.relative(skillDir, fullPath));

      if (!relPath || relPath.startsWith("..")) {
        continue;
      }

      if (
        shouldIgnoreByPatterns(relPath, BUILTIN_IGNORE_GLOBS) ||
        shouldIgnoreByPatterns(relPath, gitignorePatterns)
      ) {
        continue;
      }

      if (entry.isSymbolicLink()) {
        continue;
      }

      if (entry.isDirectory()) {
        const subError = await walk(fullPath);
        if (subError) {
          return subError;
        }
        continue;
      }

      if (!entry.isFile()) {
        continue;
      }

      const info = await stat(fullPath);
      if (info.size > limits.maxIndividualFileBytes) {
        return { errorStatus: "capture_skipped_size_limit" };
      }

      files.push({ relPath, fullPath, size: info.size });

      if (files.length > limits.maxFileCount) {
        return { errorStatus: "capture_skipped_file_limit" };
      }

      totalBytes += info.size;
      if (totalBytes > limits.maxUncompressedBytes) {
        return { errorStatus: "capture_skipped_size_limit" };
      }
    }

    return null;
  }

  const walkError = await walk(skillDir);
  if (walkError?.errorStatus) {
    return { errorStatus: walkError.errorStatus };
  }

  files.sort((a, b) => byteStableCompare(a.relPath, b.relPath));

  return {
    files,
    totalBytes,
    estimatedTooLargeForCompression: totalBytes > limits.maxCompressedBytes,
  };
}

function normalizeFileForHash(relPath: string, contentBuffer: Buffer): Buffer {
  const asText = decodeUtf8Strict(contentBuffer);
  if (asText == null) {
    return contentBuffer;
  }

  if (relPath === "SKILL.md") {
    return Buffer.from(
      stripRegistryManagedFrontmatter(asText.replace(/\r\n/g, "\n")),
      "utf8",
    );
  }

  return Buffer.from(asText.replace(/\r\n/g, "\n"), "utf8");
}

function buildCanonicalContentSha256FromFiles(
  files: CanonicalFileWithContent[],
): string {
  const hasher = crypto.createHash("sha256");

  for (const file of files) {
    const normalized = normalizeFileForHash(file.relPath, file.content);
    hasher.update(file.relPath, "utf8");
    hasher.update("\n", "utf8");
    hasher.update(normalized);
    hasher.update("\n", "utf8");
  }

  return hasher.digest("hex");
}

async function collectCanonicalFileSnapshot(
  skillDir: string,
  limits: CaptureLimits = CAPTURE_LIMITS,
): Promise<CanonicalSnapshotInternal> {
  const collected = await collectCanonicalFiles(skillDir, limits);
  if (collected.errorStatus != null) {
    return {
      errorStatus: collected.errorStatus,
      files: [],
      fileCount: 0,
      totalBytes: 0,
      contentSha256: null,
      zipBuffer: null,
    };
  }

  if (collected.estimatedTooLargeForCompression) {
    return {
      errorStatus: "capture_skipped_size_limit",
      files: [],
      fileCount: collected.files.length,
      totalBytes: collected.totalBytes,
      contentSha256: null,
      zipBuffer: null,
    };
  }

  const files: CanonicalFileWithContent[] = [];
  for (const file of collected.files) {
    const content = await readFile(file.fullPath);
    files.push({ relPath: file.relPath, content });
  }

  const zipBuffer = buildDeterministicZipEntries(files);
  if (zipBuffer.length > limits.maxCompressedBytes) {
    return {
      errorStatus: "capture_skipped_size_limit",
      files,
      fileCount: files.length,
      totalBytes: collected.totalBytes,
      contentSha256: null,
      zipBuffer: null,
    };
  }

  return {
    errorStatus: null,
    files,
    fileCount: files.length,
    totalBytes: collected.totalBytes,
    contentSha256: buildCanonicalContentSha256FromFiles(files),
    zipBuffer,
  };
}

export async function buildCanonicalArchiveSnapshot(
  skillDir: string,
  limits: CaptureLimits = CAPTURE_LIMITS,
): Promise<CanonicalArchiveSnapshot> {
  const snapshot = await collectCanonicalFileSnapshot(skillDir, limits);
  if (snapshot.errorStatus) {
    return {
      errorStatus: snapshot.errorStatus,
      contentSha256: null,
      zipBuffer: null,
      fileCount: snapshot.fileCount,
      totalBytes: snapshot.totalBytes,
    };
  }

  return {
    errorStatus: null,
    contentSha256: snapshot.contentSha256,
    zipBuffer: snapshot.zipBuffer,
    fileCount: snapshot.fileCount,
    totalBytes: snapshot.totalBytes,
  };
}

export async function computeCanonicalContentSha256(
  skillDir: string,
  limits: CaptureLimits = CAPTURE_LIMITS,
): Promise<CanonicalHashResult> {
  const snapshot = await buildCanonicalArchiveSnapshot(skillDir, limits);
  return {
    errorStatus: snapshot.errorStatus,
    contentSha256: snapshot.contentSha256,
    fileCount: snapshot.fileCount,
    totalBytes: snapshot.totalBytes,
  };
}

const CRC32_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i += 1) {
    let c = i;
    for (let j = 0; j < 8; j += 1) {
      c = (c & 1) !== 0 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[i] = c >>> 0;
  }
  return table;
})();

function crc32(buffer: Buffer): number {
  let c = 0xffffffff;
  for (let i = 0; i < buffer.length; i += 1) {
    c = CRC32_TABLE[(c ^ buffer[i]) & 0xff] ^ (c >>> 8);
  }
  return (c ^ 0xffffffff) >>> 0;
}

function buildDeterministicZipEntries(
  files: CanonicalFileWithContent[],
): Buffer {
  const localChunks: Buffer[] = [];
  const centralChunks: Buffer[] = [];
  let offset = 0;

  const dosTime = 0;
  const dosDate = 33;

  for (const file of files) {
    const name = Buffer.from(file.relPath, "utf8");
    const data = file.content;
    const size = data.length;
    const crc = crc32(data);

    const localHeader = Buffer.alloc(30);
    localHeader.writeUInt32LE(0x04034b50, 0);
    localHeader.writeUInt16LE(20, 4);
    localHeader.writeUInt16LE(0x0800, 6);
    localHeader.writeUInt16LE(0, 8);
    localHeader.writeUInt16LE(dosTime, 10);
    localHeader.writeUInt16LE(dosDate, 12);
    localHeader.writeUInt32LE(crc, 14);
    localHeader.writeUInt32LE(size, 18);
    localHeader.writeUInt32LE(size, 22);
    localHeader.writeUInt16LE(name.length, 26);
    localHeader.writeUInt16LE(0, 28);

    localChunks.push(localHeader, name, data);

    const centralHeader = Buffer.alloc(46);
    centralHeader.writeUInt32LE(0x02014b50, 0);
    centralHeader.writeUInt16LE(20, 4);
    centralHeader.writeUInt16LE(20, 6);
    centralHeader.writeUInt16LE(0x0800, 8);
    centralHeader.writeUInt16LE(0, 10);
    centralHeader.writeUInt16LE(dosTime, 12);
    centralHeader.writeUInt16LE(dosDate, 14);
    centralHeader.writeUInt32LE(crc, 16);
    centralHeader.writeUInt32LE(size, 20);
    centralHeader.writeUInt32LE(size, 24);
    centralHeader.writeUInt16LE(name.length, 28);
    centralHeader.writeUInt16LE(0, 30);
    centralHeader.writeUInt16LE(0, 32);
    centralHeader.writeUInt16LE(0, 34);
    centralHeader.writeUInt16LE(0, 36);
    centralHeader.writeUInt32LE(0, 38);
    centralHeader.writeUInt32LE(offset, 42);

    centralChunks.push(centralHeader, name);

    offset += localHeader.length + name.length + size;
  }

  const centralDirectory = Buffer.concat(centralChunks);
  const localSection = Buffer.concat(localChunks);

  const endRecord = Buffer.alloc(22);
  endRecord.writeUInt32LE(0x06054b50, 0);
  endRecord.writeUInt16LE(0, 4);
  endRecord.writeUInt16LE(0, 6);
  endRecord.writeUInt16LE(files.length, 8);
  endRecord.writeUInt16LE(files.length, 10);
  endRecord.writeUInt32LE(centralDirectory.length, 12);
  endRecord.writeUInt32LE(localSection.length, 16);
  endRecord.writeUInt16LE(0, 20);

  return Buffer.concat([localSection, centralDirectory, endRecord]);
}

export async function createDeterministicZipBuffer(
  skillDir: string,
  limits: CaptureLimits = CAPTURE_LIMITS,
): Promise<DeterministicZipResult> {
  const snapshot = await buildCanonicalArchiveSnapshot(skillDir, limits);
  return {
    errorStatus: snapshot.errorStatus,
    zipBuffer: snapshot.zipBuffer,
    fileCount: snapshot.fileCount,
    totalBytes: snapshot.totalBytes,
  };
}

function toHeaderValue(value: any): string | null {
  const normalized = normalizeString(value);
  return normalized ?? null;
}

export interface CaptureUploadRequestOptions {
  serverURL?: string | null;
  gramKey?: string | null;
  gramProject?: string | null;
  claudeSessionID?: string | null;
  endpointPath?: string | null;
}

export function buildCaptureUploadRequest(
  skill: SkillUploadMetadata,
  archiveBuffer: Buffer,
  options: CaptureUploadRequestOptions = {},
): CaptureUploadRequest | null {
  if (
    !isRecord(skill) ||
    !Buffer.isBuffer(archiveBuffer) ||
    archiveBuffer.length === 0
  ) {
    return null;
  }

  const archiveSha256 = crypto
    .createHash("sha256")
    .update(archiveBuffer)
    .digest("hex");

  const serverURL =
    toHeaderValue(options.serverURL) ?? "https://app.getgram.ai";
  const gramKey = toHeaderValue(options.gramKey);
  const gramProject = toHeaderValue(options.gramProject);
  const claudeSessionID = toHeaderValue(options.claudeSessionID);
  const endpointPath =
    toHeaderValue(options.endpointPath) ?? "/rpc/skills.capture";

  const maybeHeaders: Record<string, string | null> = {
    "Content-Type": "application/zip",
    "Content-Length": String(archiveBuffer.length),
    "X-Gram-Skill-Name": toHeaderValue(skill.name),
    "X-Gram-Skill-Scope": toHeaderValue(skill.scope),
    "X-Gram-Skill-Discovery-Root": toHeaderValue(skill.discovery_root),
    "X-Gram-Skill-Source-Type": toHeaderValue(skill.source_type),
    "X-Gram-Skill-Content-Sha256": archiveSha256,
    "X-Gram-Skill-Asset-Format": toHeaderValue(skill.asset_format),
    "X-Gram-Skill-Resolution-Status": toHeaderValue(skill.resolution_status),
    "Gram-Key": gramKey,
    "Gram-Project": claudeSessionID ? null : gramProject,
    "X-Gram-Claude-Session-ID": claudeSessionID,
  };

  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(maybeHeaders)) {
    if (value != null) {
      headers[key] = value;
    }
  }

  return {
    method: "POST",
    url: `${serverURL.replace(/\/$/, "")}${endpointPath}`,
    headers,
    body: archiveBuffer,
  };
}
