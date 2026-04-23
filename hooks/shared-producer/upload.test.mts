import assert from "node:assert/strict";
import test from "node:test";
import { createHash } from "node:crypto";

import {
  executeUploadRequest,
  isUploadRequestReady,
  requestFromSerializable,
  runUploadWorkerFromFile,
  type UploadFetch,
} from "./upload.mts";
import { mkdtemp, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

function requireValue<T>(
  value: T | null | undefined,
  message: string,
): NonNullable<T> {
  assert.ok(value != null, message);
  return value as NonNullable<T>;
}

test("isUploadRequestReady validates shape", () => {
  const good = {
    method: "POST" as const,
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
    method: "POST" as const,
    url: "https://example.com",
    headers: {},
    body: Buffer.from("zip"),
  };

  const originalFetch = globalThis.fetch;
  // @ts-expect-error test override
  globalThis.fetch = undefined;

  try {
    const result = await executeUploadRequest(req, {
      fetchImpl: undefined,
    });
    assert.equal(result.ok, false);
    assert.equal(result.skipped, true);
    assert.equal(result.reason, "fetch_unavailable");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("executeUploadRequest uses provided fetch implementation", async () => {
  const req = {
    method: "POST" as const,
    url: "https://example.com/rpc/skills.capture",
    headers: { "X-Test": "1" },
    body: Buffer.from("zip-content"),
  };

  let captured: {
    url: string;
    opts?: {
      method?: string;
      headers?: Record<string, string>;
      body?: Buffer;
      signal?: AbortSignal;
    };
  } | null = null;

  const fakeFetch: UploadFetch = async (url, opts) => {
    captured = { url, opts };
    return { ok: true, status: 202 };
  };

  const result = await executeUploadRequest(req, {
    fetchImpl: fakeFetch,
    timeoutMs: 5000,
  });
  assert.equal(result.ok, true);
  assert.equal(result.status, 202);

  assert.ok(captured, "expected captured request");
  const capturedRequest = captured as {
    url: string;
    opts?: { method?: string };
  };
  assert.equal(capturedRequest.url, req.url);
  assert.equal(capturedRequest.opts?.method, "POST");
});

test("executeUploadRequest returns network_error when fetch throws", async () => {
  const req = {
    method: "POST" as const,
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

  const req = requireValue(
    requestFromSerializable(serialized, {
      gramKey: "k",
      gramProject: "p",
    }),
    "expected decoded request",
  );
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

  let seenHeaders: Record<string, string> | null = null;
  const verifyingFetch: UploadFetch = async (_url, options) => {
    seenHeaders = options?.headers ?? null;
    return { ok: true, status: 200 };
  };

  const result = await runUploadWorkerFromFile(file, {
    fetchImpl: verifyingFetch,
    timeoutMs: 5000,
    gramKey: "real-key",
    gramProject: "real-project",
  });

  assert.equal(result.ok, true);
  assert.equal(result.status, 200);

  const headers = requireValue(seenHeaders, "expected seen headers");
  assert.equal(headers["Gram-Key"], "real-key");
  assert.equal(headers["Gram-Project"], "real-project");
});

test("runUploadWorkerFromFile falls back to GRAM_API_KEY env when gramKey option missing", async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "gram-upload-worker-test-"));
  const file = path.join(dir, "req-env.json");

  const body = Buffer.from("zip");
  const serialized = {
    method: "POST",
    url: "https://example.com/rpc/skills.capture",
    headers: {
      "Content-Type": "application/zip",
      "X-Gram-Skill-Content-Sha256": createHash("sha256")
        .update(body)
        .digest("hex"),
    },
    bodyBase64: body.toString("base64"),
  };

  await writeFile(file, JSON.stringify(serialized), "utf8");

  let seenHeaders: Record<string, string> | null = null;
  const fakeFetch: UploadFetch = async (_url, options) => {
    seenHeaders = options?.headers ?? null;
    return { ok: true, status: 200 };
  };

  const originalApiKey = process.env.GRAM_API_KEY;
  const originalLegacyKey = process.env.GRAM_KEY;
  process.env.GRAM_API_KEY = "env-api-key";
  delete process.env.GRAM_KEY;

  try {
    const result = await runUploadWorkerFromFile(file, {
      fetchImpl: fakeFetch,
      timeoutMs: 5000,
      gramProject: "real-project",
    });

    assert.equal(result.ok, true);

    const headers = requireValue(seenHeaders, "expected seen headers");
    assert.equal(headers["Gram-Key"], "env-api-key");
    assert.equal(headers["Gram-Project"], "real-project");
  } finally {
    if (originalApiKey == null) {
      delete process.env.GRAM_API_KEY;
    } else {
      process.env.GRAM_API_KEY = originalApiKey;
    }

    if (originalLegacyKey == null) {
      delete process.env.GRAM_KEY;
    } else {
      process.env.GRAM_KEY = originalLegacyKey;
    }
  }
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
