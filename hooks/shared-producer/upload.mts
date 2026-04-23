import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFile, unlink, writeFile } from "node:fs/promises";

import type { CaptureUploadRequest } from "./packaging.mts";
import { asNonEmptyString, isJsonObject, type JsonObject } from "./types.mts";

type UploadRequestInit = {
  method?: string;
  headers?: Record<string, string>;
  body?: BodyInit | null;
  signal?: AbortSignal;
};

type UploadFailureReason =
  | "invalid_upload_request"
  | "fetch_unavailable"
  | "network_error"
  | "invalid_request_file"
  | "spawn_failed";

export type UploadExecutionResult =
  | { ok: true; skipped: false; status: number }
  | {
      ok: false;
      skipped: true;
      reason:
        | "invalid_upload_request"
        | "fetch_unavailable"
        | "invalid_request_file";
    }
  | {
      ok: false;
      skipped: false;
      reason: "network_error";
      error: string;
      status?: number;
    };

export type UploadFetch = (
  input: string,
  init?: {
    method?: string;
    headers?: Record<string, string>;
    body?: Buffer;
    signal?: AbortSignal;
  },
) => Promise<{ ok: boolean; status: number }>;

export interface ExecuteUploadOptions {
  fetchImpl?: UploadFetch;
  timeoutMs?: number;
}

export interface RequestFromSerializableOptions {
  gramKey?: string | null;
  gramProject?: string | null;
}

export interface RunUploadWorkerOptions extends ExecuteUploadOptions {
  gramKey?: string | null;
  gramProject?: string | null;
}

export interface SpawnDetachedUploadOptions {
  nodeBin?: string;
  workerPath?: string;
  gramKey?: string | null;
  gramProject?: string | null;
}

type SerializableRequest = {
  method: "POST";
  url: string;
  headers: Record<string, string>;
  bodyBase64: string;
};

function isRecord(value: any): value is JsonObject {
  return isJsonObject(value);
}

function isStringRecord(value: any): value is Record<string, string> {
  if (!isJsonObject(value)) {
    return false;
  }
  return Object.values(value).every((entry) => typeof entry === "string");
}

export function isUploadRequestReady(
  uploadRequest: any,
): uploadRequest is CaptureUploadRequest {
  if (!isRecord(uploadRequest)) {
    return false;
  }

  if (uploadRequest.method !== "POST") {
    return false;
  }

  if (typeof uploadRequest.url !== "string" || uploadRequest.url.length === 0) {
    return false;
  }

  if (!isStringRecord(uploadRequest.headers)) {
    return false;
  }

  if (!Buffer.isBuffer(uploadRequest.body) || uploadRequest.body.length === 0) {
    return false;
  }

  return true;
}

export async function executeUploadRequest(
  uploadRequest: any,
  options: ExecuteUploadOptions = {},
): Promise<UploadExecutionResult> {
  if (!isUploadRequestReady(uploadRequest)) {
    return { ok: false, skipped: true, reason: "invalid_upload_request" };
  }

  const fetchImpl: UploadFetch | undefined =
    options.fetchImpl ??
    (typeof globalThis.fetch === "function"
      ? (input, init) =>
          globalThis.fetch(input, {
            ...(init ?? {}),
            body: init?.body as BodyInit | null | undefined,
          })
      : undefined);
  if (typeof fetchImpl !== "function") {
    return { ok: false, skipped: true, reason: "fetch_unavailable" };
  }

  const timeoutMs =
    typeof options.timeoutMs === "number" && Number.isFinite(options.timeoutMs)
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

    if (response.ok) {
      return {
        ok: true,
        skipped: false,
        status: response.status,
      };
    }

    return {
      ok: false,
      skipped: false,
      reason: "network_error",
      status: response.status,
      error: `http_${response.status}`,
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

function requestToSerializable(
  uploadRequest: CaptureUploadRequest,
): SerializableRequest {
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

export function requestFromSerializable(
  serialized: any,
  options: RequestFromSerializableOptions = {},
): CaptureUploadRequest | null {
  if (!isRecord(serialized)) {
    return null;
  }

  if (
    serialized.method !== "POST" ||
    !asNonEmptyString(serialized.url) ||
    !isStringRecord(serialized.headers) ||
    typeof serialized.bodyBase64 !== "string"
  ) {
    return null;
  }

  const headers: Record<string, string> = { ...serialized.headers };
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
    method: "POST",
    url: asNonEmptyString(serialized.url) ?? "",
    headers,
    body: Buffer.from(serialized.bodyBase64, "base64"),
  };
}

async function writeWorkerRequestFile(
  uploadRequest: CaptureUploadRequest,
): Promise<string> {
  const requestPath = path.join(
    os.tmpdir(),
    `gram-skills-upload-${randomUUID()}.json`,
  );

  const payload = JSON.stringify(requestToSerializable(uploadRequest));
  await writeFile(requestPath, payload, { encoding: "utf8", mode: 0o600 });

  return requestPath;
}

export async function runUploadWorkerFromFile(
  requestPath: string,
  options: RunUploadWorkerOptions = {},
): Promise<UploadExecutionResult> {
  try {
    const raw = await readFile(requestPath, "utf8");
    const serialized: any = JSON.parse(raw);
    const request = requestFromSerializable(serialized, {
      gramKey:
        options.gramKey ?? process.env.GRAM_API_KEY ?? process.env.GRAM_KEY,
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

export async function spawnDetachedUploadWorker(
  uploadRequest: any,
  options: SpawnDetachedUploadOptions = {},
): Promise<
  { spawned: true } | { spawned: false; reason: UploadFailureReason }
> {
  if (!isUploadRequestReady(uploadRequest)) {
    return { spawned: false, reason: "invalid_upload_request" };
  }

  const requestFile = await writeWorkerRequestFile(uploadRequest);

  const nodeBin = options.nodeBin ?? process.execPath;
  const moduleDir = path.dirname(fileURLToPath(import.meta.url));
  const workerPath =
    options.workerPath ?? path.join(moduleDir, "producer-upload-worker.mts");

  try {
    const workerEnv = {
      ...process.env,
      ...(typeof options.gramKey === "string" && options.gramKey.length > 0
        ? { GRAM_API_KEY: options.gramKey }
        : {}),
      ...(typeof options.gramProject === "string" &&
      options.gramProject.length > 0
        ? { GRAM_PROJECT_SLUG: options.gramProject }
        : {}),
    };

    const child = spawn(nodeBin, [workerPath, "--request-file", requestFile], {
      detached: true,
      stdio: "ignore",
      env: workerEnv,
    });
    child.unref();
    return { spawned: true };
  } catch {
    await unlink(requestFile).catch(() => {});
    return { spawned: false, reason: "spawn_failed" };
  }
}
