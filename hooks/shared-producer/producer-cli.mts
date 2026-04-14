#!/usr/bin/env node

import os from "node:os";
import { readFile } from "node:fs/promises";
import { stdin, stderr, stdout } from "node:process";

import {
  buildEnrichedHookPayload,
  resolveAgent,
  resolveResolutionStatus,
  type SkillMetadataEnvelope,
} from "./producer-core.mts";
import { markUploadSeen, shouldSuppressUpload } from "./cache.mts";
import { spawnDetachedUploadWorker } from "./upload.mts";
import { isJsonObject, type JsonObject } from "./types.mts";

interface PayloadSource {
  kind: "file" | "stdin";
  value: string | null;
}

function isRecord(value: any): value is JsonObject {
  return isJsonObject(value);
}

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
    const parsed: any = JSON.parse(trimmed);
    return isJsonObject(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function resolveRawPayloadSource(
  argv: readonly string[] = process.argv.slice(2),
  env: NodeJS.ProcessEnv = process.env,
): PayloadSource {
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--payload-file") {
      return { kind: "file", value: argv[i + 1] ?? null };
    }
    if (arg.startsWith("--payload-file=")) {
      return { kind: "file", value: arg.slice("--payload-file=".length) };
    }
  }

  const fromEnv = env.GRAM_HOOK_PAYLOAD_FILE;
  if (fromEnv) {
    return { kind: "file", value: fromEnv };
  }

  return { kind: "stdin", value: null };
}

async function loadPayloadFromFile(
  filePath: string | null,
): Promise<JsonObject | null> {
  if (!filePath) {
    return null;
  }

  try {
    const content = await readFile(filePath, "utf8");
    return parseJSONOrNull(content);
  } catch {
    return null;
  }
}

function writeLine(stream: NodeJS.WritableStream, line: string): void {
  stream.write(`${line}\n`);
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

function isSkillMetadata(
  value: any,
): value is SkillMetadataEnvelope["skills"][number] {
  return (
    isRecord(value) &&
    typeof value.name === "string" &&
    typeof value.source_type === "string" &&
    typeof value.resolution_status === "string"
  );
}

function extractSkillForCache(
  payload: JsonObject,
):
  | (SkillMetadataEnvelope["skills"][number] & { content_sha256?: string })
  | null {
  if (!isRecord(payload)) {
    return null;
  }

  const additionalData = payload.additional_data;
  if (!isRecord(additionalData) || !Array.isArray(additionalData.skills)) {
    return null;
  }

  const first = additionalData.skills[0];
  if (!isSkillMetadata(first)) {
    return null;
  }

  return first;
}

async function main(): Promise<void> {
  const agentResolution = resolveAgent();

  if (agentResolution.error) {
    writeLine(stderr, `[gram-skills-producer] ${agentResolution.error}`);
  }

  const payloadSource = resolveRawPayloadSource();
  const rawPayload =
    payloadSource.kind === "file"
      ? await loadPayloadFromFile(payloadSource.value)
      : parseJSONOrNull(await readStdin());

  if (!rawPayload) {
    writeLine(stdout, "{}");
    return;
  }

  const resolutionStatus = resolveResolutionStatus();

  const enrichedResult = await buildEnrichedHookPayload(rawPayload, {
    resolutionStatus: resolutionStatus ?? undefined,
    agent: agentResolution.agent,
    projectDir: process.cwd(),
    homeDir: os.homedir(),
    serverURL: process.env.GRAM_HOOKS_SERVER_URL,
    gramKey: process.env.GRAM_API_KEY,
    gramProject: process.env.GRAM_PROJECT_SLUG,
  });

  writeLine(stdout, JSON.stringify(enrichedResult.payload));

  const uploadEnabled = process.env.GRAM_SKILLS_UPLOAD_ENABLED === "true";
  if (uploadEnabled && enrichedResult.uploadRequest) {
    const skill = extractSkillForCache(enrichedResult.payload);

    const suppress = await shouldSuppressUpload({
      homeDir: os.homedir(),
      ttlMs: parseTTLFromEnv(),
      project: process.env.GRAM_PROJECT_SLUG,
      skillName: skill?.name,
      canonicalContentSha256: skill?.content_sha256,
    }).catch(() => false);

    if (!suppress) {
      const spawned = await spawnDetachedUploadWorker(
        enrichedResult.uploadRequest,
      ).catch((error: any) => {
        writeLine(
          stderr,
          `[gram-skills-producer] upload worker spawn failed: ${error instanceof Error ? error.message : String(error)}`,
        );
        return { spawned: false, reason: "spawn_failed" };
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
  }
}

await main().catch((err: any) => {
  writeLine(
    stderr,
    `[gram-skills-producer] unexpected error: ${err instanceof Error ? err.message : String(err)}`,
  );
  writeLine(stdout, "{}");
});
