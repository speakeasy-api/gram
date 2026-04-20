#!/usr/bin/env node

import os from "node:os";
import { stdin } from "node:process";

import {
  buildEnrichedHookPayload,
  resolveAgent,
  resolveResolutionStatus,
} from "./producer-core.mts";
import { markUploadSeen, shouldSuppressUpload } from "./cache.mts";
import { spawnDetachedUploadWorker } from "./upload.mts";
import { isJsonObject, type JsonObject } from "./types.mts";
import { logHookDebug } from "./debug-log.mts";

async function readStdin(): Promise<string> {
  if (stdin.isTTY) {
    return "";
  }

  const chunks: Buffer[] = [];
  for await (const chunk of stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }

  return Buffer.concat(chunks).toString("utf8");
}

function parseJSONOrNull(input: string): JsonObject | null {
  const trimmed = input.trim();
  if (!trimmed) {
    return null;
  }

  try {
    const parsed: unknown = JSON.parse(trimmed);
    return isJsonObject(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function parseTTLFromEnv(
  env: NodeJS.ProcessEnv = process.env,
): number | undefined {
  const raw = Number(env.GRAM_SKILLS_UPLOAD_CACHE_TTL_MS);
  if (!Number.isFinite(raw) || raw <= 0) {
    return undefined;
  }
  return raw;
}

function isSkillMetadata(value: unknown): value is {
  name: string;
  source_type: string;
  resolution_status: string;
  content_sha256?: string;
} {
  return (
    isJsonObject(value) &&
    typeof value.name === "string" &&
    typeof value.source_type === "string" &&
    typeof value.resolution_status === "string"
  );
}

function extractSkillForCache(payload: JsonObject): {
  name: string;
  content_sha256?: string;
} | null {
  const additionalData = payload.additional_data;
  if (!isJsonObject(additionalData) || !Array.isArray(additionalData.skills)) {
    return null;
  }

  const first = additionalData.skills[0];
  if (!isSkillMetadata(first)) {
    return null;
  }

  return first;
}

function buildHooksPostHeaders(agent: string | null): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (agent !== "cursor") {
    return headers;
  }

  const gramKey = process.env.GRAM_API_KEY ?? process.env.GRAM_KEY;
  const gramProject = process.env.GRAM_PROJECT_SLUG;

  if (gramKey) {
    headers["Gram-Key"] = gramKey;
  }
  if (gramProject) {
    headers["Gram-Project"] = gramProject;
  }

  return headers;
}

async function postToHooksEndpoint(
  payloadBody: string,
  agent: string | null,
): Promise<void> {
  const serverURL =
    process.env.GRAM_HOOKS_SERVER_URL ?? "https://app.getgram.ai";
  const endpoint =
    process.env.GRAM_HOOKS_ENDPOINT ??
    (agent === "cursor" ? "/rpc/hooks.cursor" : "/rpc/hooks.claude");
  const url = `${serverURL}${endpoint}`;
  const headers = buildHooksPostHeaders(agent);

  await logHookDebug("send-hook", "hooks_post_start", {
    agent,
    endpoint,
    url,
    payloadBytes: Buffer.byteLength(payloadBody, "utf8"),
    hasGramKeyHeader: Boolean(headers["Gram-Key"]),
    hasGramProjectHeader: Boolean(headers["Gram-Project"]),
  });

  try {
    const response = await fetch(url, {
      method: "POST",
      headers,
      body: payloadBody,
    });

    await logHookDebug("send-hook", "hooks_post_result", {
      ok: response.ok,
      status: response.status,
      endpoint,
      url,
    });
  } catch (error) {
    await logHookDebug("send-hook", "hooks_post_error", {
      endpoint,
      url,
      error: error instanceof Error ? error.message : String(error),
    });
    throw error;
  }
}

async function maybeSpawnUploadWorker(
  payload: JsonObject,
  uploadRequest: any,
): Promise<void> {
  if (process.env.GRAM_SKILLS_UPLOAD_ENABLED !== "true") {
    await logHookDebug("send-hook", "upload_skipped_disabled", null);
    return;
  }
  if (!uploadRequest) {
    await logHookDebug("send-hook", "upload_skipped_missing_request", null);
    return;
  }

  const skill = extractSkillForCache(payload);

  const suppress = await shouldSuppressUpload({
    homeDir: os.homedir(),
    ttlMs: parseTTLFromEnv(),
    project: process.env.GRAM_PROJECT_SLUG,
    skillName: skill?.name,
    canonicalContentSha256: skill?.content_sha256,
  }).catch(() => false);

  if (suppress) {
    await logHookDebug("send-hook", "upload_suppressed_recent_duplicate", {
      skillName: skill?.name ?? null,
      canonicalContentSha256: skill?.content_sha256 ?? null,
      project: process.env.GRAM_PROJECT_SLUG ?? null,
    });
    return;
  }

  const spawned = await spawnDetachedUploadWorker(uploadRequest, {
    gramKey: process.env.GRAM_API_KEY ?? process.env.GRAM_KEY,
    gramProject: process.env.GRAM_PROJECT_SLUG,
  }).catch(() => {
    return { spawned: false as const, reason: "spawn_failed" as const };
  });

  await logHookDebug("send-hook", "upload_worker_spawn_result", {
    ...spawned,
    skillName: skill?.name ?? null,
    canonicalContentSha256: skill?.content_sha256 ?? null,
    hasGramKey: Boolean(process.env.GRAM_API_KEY ?? process.env.GRAM_KEY),
    gramProject: process.env.GRAM_PROJECT_SLUG ?? null,
  });

  if (spawned?.spawned) {
    await markUploadSeen({
      homeDir: os.homedir(),
      ttlMs: parseTTLFromEnv(),
      project: process.env.GRAM_PROJECT_SLUG,
      skillName: skill?.name,
      canonicalContentSha256: skill?.content_sha256,
    }).catch(() => {});
  }
}

async function main(): Promise<void> {
  try {
    const stdinBody = await readStdin();
    const rawPayload = parseJSONOrNull(stdinBody);
    if (!rawPayload) {
      await logHookDebug("send-hook", "no_payload", {
        stdinBytes: Buffer.byteLength(stdinBody, "utf8"),
      });
      return;
    }

    const agentResolution = resolveAgent();
    const resolutionStatus = resolveResolutionStatus();

    await logHookDebug("send-hook", "start", {
      hookEventName:
        typeof rawPayload.hook_event_name === "string"
          ? rawPayload.hook_event_name
          : null,
      toolName:
        typeof rawPayload.tool_name === "string" ? rawPayload.tool_name : null,
      hasAdditionalData: isJsonObject(rawPayload.additional_data),
      agent: agentResolution.agent,
      agentSource: agentResolution.source,
      agentError: agentResolution.error,
      resolutionStatusOverride: resolutionStatus,
      hasGramKey: Boolean(process.env.GRAM_API_KEY ?? process.env.GRAM_KEY),
      gramProject: process.env.GRAM_PROJECT_SLUG ?? null,
    });

    const enrichedResult = await buildEnrichedHookPayload(rawPayload, {
      resolutionStatus: resolutionStatus ?? undefined,
      agent: agentResolution.agent,
      projectDir: process.cwd(),
      homeDir: os.homedir(),
      serverURL: process.env.GRAM_HOOKS_SERVER_URL,
      gramKey: process.env.GRAM_API_KEY ?? process.env.GRAM_KEY,
      gramProject: process.env.GRAM_PROJECT_SLUG,
    });

    await logHookDebug("send-hook", "enriched", {
      hasUploadRequest: Boolean(enrichedResult.uploadRequest),
      skillsCount:
        isJsonObject(enrichedResult.payload.additional_data) &&
        Array.isArray(enrichedResult.payload.additional_data.skills)
          ? enrichedResult.payload.additional_data.skills.length
          : 0,
      firstSkill: extractSkillForCache(enrichedResult.payload),
    });

    const payloadBody = JSON.stringify(enrichedResult.payload);

    await postToHooksEndpoint(payloadBody, agentResolution.agent);
    await maybeSpawnUploadWorker(
      enrichedResult.payload,
      enrichedResult.uploadRequest,
    );

    await logHookDebug("send-hook", "done", null);
  } catch (error) {
    await logHookDebug("send-hook", "fatal_error", {
      error: error instanceof Error ? error.message : String(error),
    });
    // fail-open: hook failures should never block local Claude session flow
  }
}

await main();
