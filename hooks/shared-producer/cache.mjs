import os from "node:os";
import path from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";

const CACHE_DIRNAME = ".gram";
const CACHE_FILENAME = "skills-upload-cache.json";
const DEFAULT_TTL_MS = 15 * 60 * 1000;
const MAX_ENTRIES = 2000;

function normalizeString(value) {
  if (typeof value !== "string") {
    return null;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function toNumberOr(defaultValue, value) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) {
    return defaultValue;
  }
  return num;
}

export function getDefaultCachePath(homeDir = os.homedir()) {
  return path.join(homeDir, CACHE_DIRNAME, CACHE_FILENAME);
}

function parseCacheIdentity({ project, skillName, canonicalContentSha256 }) {
  const parsed = {
    project: normalizeString(project),
    skillName: normalizeString(skillName),
    canonicalContentSha256: normalizeString(canonicalContentSha256),
  };

  return {
    ...parsed,
    isComplete:
      parsed.project != null &&
      parsed.skillName != null &&
      parsed.canonicalContentSha256 != null,
  };
}

export function computeCacheKey({
  project,
  skillName,
  canonicalContentSha256,
}) {
  const identity = parseCacheIdentity({
    project,
    skillName,
    canonicalContentSha256,
  });

  if (!identity.isComplete) {
    return null;
  }

  return `${identity.project}::${identity.skillName}::${identity.canonicalContentSha256}`;
}

function parseCache(raw) {
  if (!isRecord(raw) || !isRecord(raw.entries)) {
    return { entries: {} };
  }
  return { entries: raw.entries };
}

async function loadCache(cachePath) {
  try {
    const raw = await readFile(cachePath, "utf8");
    const parsed = JSON.parse(raw);
    return parseCache(parsed);
  } catch {
    return { entries: {} };
  }
}

async function saveCache(cachePath, cache) {
  const dir = path.dirname(cachePath);
  await mkdir(dir, { recursive: true, mode: 0o700 }).catch(() => {});
  await writeFile(cachePath, JSON.stringify(cache), {
    encoding: "utf8",
    mode: 0o600,
  });
}

function pruneEntries(entries, nowMs, ttlMs) {
  const fresh = [];

  for (const [key, entry] of Object.entries(entries)) {
    if (!isRecord(entry)) {
      continue;
    }
    const seenAtMs = Number(entry.seenAtMs);
    if (!Number.isFinite(seenAtMs)) {
      continue;
    }
    if (nowMs - seenAtMs > ttlMs) {
      continue;
    }
    fresh.push([key, seenAtMs]);
  }

  fresh.sort((a, b) => b[1] - a[1]);
  return Object.fromEntries(
    fresh.slice(0, MAX_ENTRIES).map(([key, seenAtMs]) => [key, { seenAtMs }]),
  );
}

export async function shouldSuppressUpload(options = {}) {
  const ttlMs = toNumberOr(DEFAULT_TTL_MS, options.ttlMs);
  const nowMs = toNumberOr(Date.now(), options.nowMs ?? Date.now());
  const cachePath = options.cachePath ?? getDefaultCachePath(options.homeDir);

  const key = computeCacheKey({
    project: options.project,
    skillName: options.skillName,
    canonicalContentSha256: options.canonicalContentSha256,
  });

  if (!key) {
    return false;
  }

  const cache = await loadCache(cachePath);
  const entry = cache.entries[key];
  if (!isRecord(entry) || !Number.isFinite(Number(entry.seenAtMs))) {
    return false;
  }

  return nowMs - Number(entry.seenAtMs) <= ttlMs;
}

export async function markUploadSeen(options = {}) {
  const ttlMs = toNumberOr(DEFAULT_TTL_MS, options.ttlMs);
  const nowMs = toNumberOr(Date.now(), options.nowMs ?? Date.now());
  const cachePath = options.cachePath ?? getDefaultCachePath(options.homeDir);

  const key = computeCacheKey({
    project: options.project,
    skillName: options.skillName,
    canonicalContentSha256: options.canonicalContentSha256,
  });

  if (!key) {
    return;
  }

  const cache = await loadCache(cachePath);
  const entries = {
    ...cache.entries,
    [key]: { seenAtMs: nowMs },
  };

  const pruned = pruneEntries(entries, nowMs, ttlMs);

  await saveCache(cachePath, { entries: pruned }).catch(() => {});
}

export const cacheInternals = {
  DEFAULT_TTL_MS,
  MAX_ENTRIES,
};
