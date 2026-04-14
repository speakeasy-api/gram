import assert from "node:assert/strict";
import test from "node:test";
import os from "node:os";
import path from "node:path";
import { mkdtemp } from "node:fs/promises";

import {
  cacheInternals,
  computeCacheKey,
  getDefaultCachePath,
  markUploadSeen,
  shouldSuppressUpload,
} from "./cache.mjs";

test("getDefaultCachePath uses ~/.gram/skills-upload-cache.json", () => {
  const p = getDefaultCachePath("/home/tester");
  assert.equal(p, "/home/tester/.gram/skills-upload-cache.json");
});

test("computeCacheKey is stable for same inputs", () => {
  const k1 = computeCacheKey({
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "abc",
  });
  const k2 = computeCacheKey({
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "abc",
  });
  assert.equal(k1, k2);
});

test("markUploadSeen then shouldSuppressUpload within ttl", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-cache-test-"));
  const cachePath = path.join(dir, "skills-upload-cache.json");

  await markUploadSeen({
    cachePath,
    ttlMs: cacheInternals.DEFAULT_TTL_MS,
    nowMs: 1000,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "sha",
  });

  const suppressed = await shouldSuppressUpload({
    cachePath,
    ttlMs: cacheInternals.DEFAULT_TTL_MS,
    nowMs: 1000 + 100,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "sha",
  });

  assert.equal(suppressed, true);
});

test("shouldSuppressUpload expires after ttl", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-cache-test-"));
  const cachePath = path.join(dir, "skills-upload-cache.json");

  await markUploadSeen({
    cachePath,
    ttlMs: 100,
    nowMs: 1000,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "sha",
  });

  const suppressed = await shouldSuppressUpload({
    cachePath,
    ttlMs: 100,
    nowMs: 1201,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: "sha",
  });

  assert.equal(suppressed, false);
});

test("shouldSuppressUpload returns false when identity is incomplete", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-cache-test-"));
  const cachePath = path.join(dir, "skills-upload-cache.json");

  await markUploadSeen({
    cachePath,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: null,
  });

  const missingHash = await shouldSuppressUpload({
    cachePath,
    project: "proj",
    skillName: "golang",
    canonicalContentSha256: null,
  });
  assert.equal(missingHash, false);

  await markUploadSeen({
    cachePath,
    project: null,
    skillName: "golang",
    canonicalContentSha256: "sha",
  });

  const missingProject = await shouldSuppressUpload({
    cachePath,
    project: null,
    skillName: "golang",
    canonicalContentSha256: "sha",
  });
  assert.equal(missingProject, false);

  await markUploadSeen({
    cachePath,
    project: "proj",
    skillName: "",
    canonicalContentSha256: "sha",
  });

  const missingSkill = await shouldSuppressUpload({
    cachePath,
    project: "proj",
    skillName: "",
    canonicalContentSha256: "sha",
  });
  assert.equal(missingSkill, false);
});
