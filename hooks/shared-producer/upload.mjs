import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFile, unlink, writeFile } from "node:fs/promises";

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isUploadRequestReady(uploadRequest) {
  if (!isRecord(uploadRequest)) {
    return false;
  }

  if (uploadRequest.method !== "POST") {
    return false;
  }

  if (typeof uploadRequest.url !== "string" || uploadRequest.url.length === 0) {
    return false;
  }

  if (!isRecord(uploadRequest.headers)) {
    return false;
  }

  if (!Buffer.isBuffer(uploadRequest.body) || uploadRequest.body.length === 0) {
    return false;
  }

  return true;
}

export async function executeUploadRequest(uploadRequest, options = {}) {
  if (!isUploadRequestReady(uploadRequest)) {
    return { ok: false, skipped: true, reason: "invalid_upload_request" };
  }

  const fetchImpl = options.fetchImpl ?? globalThis.fetch;
  if (typeof fetchImpl !== "function") {
    return { ok: false, skipped: true, reason: "fetch_unavailable" };
  }

  const timeoutMs = Number.isFinite(options.timeoutMs)
    ? options.timeoutMs
    : 8000;
  const signal = AbortSignal.timeout(timeoutMs);

  try {
    const response = await fetchImpl(uploadRequest.url, {
      method: uploadRequest.method,
      headers: uploadRequest.headers,
      body: uploadRequest.body,
      signal,
    });

    return {
      ok: response.ok,
      skipped: false,
      status: response.status,
    };
  } catch (error) {
    return {
      ok: false,
      skipped: false,
      reason: "network_error",
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

function requestToSerializable(uploadRequest) {
  const headers = { ...uploadRequest.headers };
  delete headers["Gram-Key"];
  delete headers["Gram-Project"];

  return {
    method: uploadRequest.method,
    url: uploadRequest.url,
    headers,
    bodyBase64: uploadRequest.body.toString("base64"),
  };
}

export function requestFromSerializable(serialized, options = {}) {
  if (!isRecord(serialized)) {
    return null;
  }

  if (
    typeof serialized.method !== "string" ||
    typeof serialized.url !== "string" ||
    !isRecord(serialized.headers) ||
    typeof serialized.bodyBase64 !== "string"
  ) {
    return null;
  }

  const headers = { ...serialized.headers };
  if (typeof options.gramKey === "string" && options.gramKey.length > 0) {
    headers["Gram-Key"] = options.gramKey;
  }
  if (
    typeof options.gramProject === "string" &&
    options.gramProject.length > 0
  ) {
    headers["Gram-Project"] = options.gramProject;
  }

  return {
    method: serialized.method,
    url: serialized.url,
    headers,
    body: Buffer.from(serialized.bodyBase64, "base64"),
  };
}

async function writeWorkerRequestFile(uploadRequest) {
  const requestPath = path.join(
    os.tmpdir(),
    `gram-skills-upload-${randomUUID()}.json`,
  );

  const payload = JSON.stringify(requestToSerializable(uploadRequest));
  await writeFile(requestPath, payload, { encoding: "utf8", mode: 0o600 });

  return requestPath;
}

export async function runUploadWorkerFromFile(requestPath, options = {}) {
  try {
    const raw = await readFile(requestPath, "utf8");
    const serialized = JSON.parse(raw);
    const request = requestFromSerializable(serialized, {
      gramKey: options.gramKey ?? process.env.GRAM_KEY,
      gramProject: options.gramProject ?? process.env.GRAM_PROJECT_SLUG,
    });
    if (!request) {
      return { ok: false, skipped: true, reason: "invalid_upload_request" };
    }

    return await executeUploadRequest(request, options);
  } catch {
    return { ok: false, skipped: true, reason: "invalid_request_file" };
  } finally {
    await unlink(requestPath).catch(() => {});
  }
}

export async function spawnDetachedUploadWorker(uploadRequest, options = {}) {
  if (!isUploadRequestReady(uploadRequest)) {
    return { spawned: false, reason: "invalid_upload_request" };
  }

  const requestFile = await writeWorkerRequestFile(uploadRequest);

  const nodeBin = options.nodeBin ?? process.execPath;
  const moduleDir = path.dirname(fileURLToPath(import.meta.url));
  const workerPath =
    options.workerPath ?? path.join(moduleDir, "producer-upload-worker.mjs");

  try {
    const child = spawn(nodeBin, [workerPath, "--request-file", requestFile], {
      detached: true,
      stdio: "ignore",
    });
    child.unref();
    return { spawned: true };
  } catch {
    await unlink(requestFile).catch(() => {});
    return { spawned: false, reason: "spawn_failed" };
  }
}
