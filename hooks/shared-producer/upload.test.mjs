import assert from "node:assert/strict";
import test from "node:test";
import { createHash } from "node:crypto";

import {
  executeUploadRequest,
  isUploadRequestReady,
  requestFromSerializable,
  runUploadWorkerFromFile,
} from "./upload.mjs";
import { mkdtemp, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

test("isUploadRequestReady validates shape", () => {
  const good = {
    method: "POST",
    url: "https://example.com/rpc/skills.capture",
    headers: { "Content-Type": "application/zip" },
    body: Buffer.from("zip"),
  };

  assert.equal(isUploadRequestReady(good), true);
  assert.equal(isUploadRequestReady({ ...good, body: "nope" }), false);
  assert.equal(isUploadRequestReady(null), false);
});

test("executeUploadRequest returns skipped when fetch unavailable", async () => {
  const req = {
    method: "POST",
    url: "https://example.com",
    headers: {},
    body: Buffer.from("zip"),
  };

  const result = await executeUploadRequest(req, { fetchImpl: {} });
  assert.equal(result.ok, false);
  assert.equal(result.skipped, true);
  assert.equal(result.reason, "fetch_unavailable");
});

test("executeUploadRequest uses provided fetch implementation", async () => {
  const req = {
    method: "POST",
    url: "https://example.com/rpc/skills.capture",
    headers: { "X-Test": "1" },
    body: Buffer.from("zip-content"),
  };

  let captured = null;
  const fakeFetch = async (url, opts) => {
    captured = { url, opts };
    return { ok: true, status: 202 };
  };

  const result = await executeUploadRequest(req, {
    fetchImpl: fakeFetch,
    timeoutMs: 5000,
  });
  assert.equal(result.ok, true);
  assert.equal(result.status, 202);
  assert.equal(captured.url, req.url);
  assert.equal(captured.opts.method, "POST");
});

test("executeUploadRequest returns network_error when fetch throws", async () => {
  const req = {
    method: "POST",
    url: "https://example.com/rpc/skills.capture",
    headers: {},
    body: Buffer.from("zip-content"),
  };

  const result = await executeUploadRequest(req, {
    fetchImpl: async () => {
      throw new Error("boom");
    },
    timeoutMs: 5000,
  });

  assert.equal(result.ok, false);
  assert.equal(result.skipped, false);
  assert.equal(result.reason, "network_error");
});

test("requestFromSerializable decodes base64 body", () => {
  const body = Buffer.from("zip");
  const serialized = {
    method: "POST",
    url: "https://example.com",
    headers: { A: "B" },
    bodyBase64: body.toString("base64"),
  };

  const req = requestFromSerializable(serialized, {
    gramKey: "k",
    gramProject: "p",
  });
  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://example.com");
  assert.equal(req.headers["Gram-Key"], "k");
  assert.equal(req.headers["Gram-Project"], "p");
  assert.equal(Buffer.compare(req.body, body), 0);
});

test("runUploadWorkerFromFile loads request and executes upload", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-upload-worker-test-"));
  const file = path.join(dir, "req.json");

  const body = Buffer.from("zip");
  const serialized = {
    method: "POST",
    url: "https://example.com/rpc/skills.capture",
    headers: {
      "Content-Type": "application/zip",
      "X-Gram-Skill-Content-Sha256": createHash("sha256")
        .update(body)
        .digest("hex"),
      "Gram-Key": "should-not-win",
    },
    bodyBase64: body.toString("base64"),
  };

  await writeFile(file, JSON.stringify(serialized), "utf8");

  const fakeFetch = async () => ({ ok: true, status: 200 });
  let seenHeaders = null;
  const verifyingFetch = async (_url, options) => {
    seenHeaders = options.headers;
    return fakeFetch();
  };

  const result = await runUploadWorkerFromFile(file, {
    fetchImpl: verifyingFetch,
    timeoutMs: 5000,
    gramKey: "real-key",
    gramProject: "real-project",
  });

  assert.equal(result.ok, true);
  assert.equal(result.status, 200);
  assert.equal(seenHeaders["Gram-Key"], "real-key");
  assert.equal(seenHeaders["Gram-Project"], "real-project");
});

test("runUploadWorkerFromFile returns invalid_request_file for bad json", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-upload-worker-test-"));
  const file = path.join(dir, "req-bad.json");

  await writeFile(file, "{not-json", "utf8");

  const result = await runUploadWorkerFromFile(file);
  assert.equal(result.ok, false);
  assert.equal(result.skipped, true);
  assert.equal(result.reason, "invalid_request_file");
});
